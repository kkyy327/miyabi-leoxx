package catalogue

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/javdb"
	"github.com/ppxb/miyabi/internal/magnet"
	"github.com/ppxb/miyabi/internal/netx"
)

const (
	javdbDeviceSetting = "javdb.device_uuid"
	javdbRouteSetting  = "javdb.route"
	// metatubePostersSetting toggles MetaTube vertical posters. It is cached
	// in memory; projections never query the database for it.
	metatubePostersSetting = "metatube.posters"
)

// MetaTubePostersSetting controls whether browse/search/detail responses
// carry MetaTube vertical posters instead of leaving Poster empty.
type MetaTubePostersSetting struct {
	Enabled bool `json:"enabled"`
}

type persistedRoute struct {
	Host      string `json:"host"`
	LatencyMS int64  `json:"latency_ms"`
}

// Service combines JavDB catalogue data with Miyabi's local library and workflow state.
type Service struct {
	database   *ent.Client
	local      LocalState
	javdb      JavDBClient
	javbus     JavBusSource
	aggregator *magnet.Aggregator

	proxy   *netx.ProxyManager
	lists   *responseCache[[]domain.Movie]
	details *responseCache[domain.MovieDetail]
	tags    *responseCache[[]domain.TagCategory]
	magnets *responseCache[[]domain.Magnet]

	persistRouteMu sync.Mutex
	// Used only to avoid redundant database writes; live state belongs to JavDB.
	lastSavedRoute persistedRoute

	metatubePublicURL string
	metatubePostersMu sync.RWMutex
	metatubePosters   MetaTubePostersSetting
}

// New creates the lazy JavDB client and restores/persists device UUID and route settings.
// On success, Close also releases the supplied magnet source.
func New(
	ctx context.Context,
	db *ent.Client,
	proxy *netx.ProxyManager,
	local LocalState,
	supplement JavBusSource,
) (*Service, error) {
	deviceUUID, found, err := database.LoadSetting[string](ctx, db, javdbDeviceSetting)
	if err != nil {
		return nil, err
	}
	if !found {
		deviceUUID, err = javdb.NewDeviceUUID()
		if err != nil {
			return nil, err
		}
		if err := database.SaveSetting(ctx, db, javdbDeviceSetting, deviceUUID); err != nil {
			return nil, err
		}
	}

	route, _, err := database.LoadSetting[persistedRoute](ctx, db, javdbRouteSetting)
	if err != nil {
		return nil, err
	}
	client, err := javdb.New(javdb.Options{
		DeviceUUID:    deviceUUID,
		Proxy:         proxy,
		CachedHost:    route.Host,
		CachedLatency: time.Duration(route.LatencyMS) * time.Millisecond,
	})
	if err != nil {
		return nil, err
	}

	service := newService(db, client, supplement, local, route)
	service.proxy = proxy
	return service, nil
}

// NewWithClients assembles explicit clients using the same cache and aggregation
// pipeline as New. JavDB is required; JavBus may be nil. Close releases both clients.
func NewWithClients(
	ctx context.Context,
	db *ent.Client,
	primary JavDBClient,
	supplement JavBusSource,
	local LocalState,
) (*Service, error) {
	route, _, err := database.LoadSetting[persistedRoute](ctx, db, javdbRouteSetting)
	if err != nil {
		return nil, err
	}
	return newService(db, primary, supplement, local, route), nil
}

// Response caches absorb repeated page loads; entries are small and short-lived
// so JavDB changes (new magnets, edited metadata) surface within minutes.
const (
	listCacheSize, listCacheTTL       = 128, time.Minute
	detailCacheSize, detailCacheTTL   = 256, 5 * time.Minute
	tagsCacheSize, tagsCacheTTL       = 5, 24 * time.Hour
	magnetsCacheSize, magnetsCacheTTL = 64, time.Minute
)

func newService(database *ent.Client, primary JavDBClient, supplement JavBusSource, local LocalState, route persistedRoute) *Service {
	sources := []magnet.Source{primary}
	if supplement != nil {
		sources = append(sources, supplement)
	}
	return &Service{
		database:       database,
		javdb:          primary,
		javbus:         supplement,
		aggregator:     magnet.NewAggregator(sources, 0),
		lists:          newResponseCache[[]domain.Movie](listCacheSize, listCacheTTL),
		details:        newResponseCache[domain.MovieDetail](detailCacheSize, detailCacheTTL),
		tags:           newResponseCache[[]domain.TagCategory](tagsCacheSize, tagsCacheTTL),
		magnets:        newResponseCache[[]domain.Magnet](magnetsCacheSize, magnetsCacheTTL),
		local:          local,
		lastSavedRoute: route,
	}
}

func (service *Service) Close() {
	service.javdb.Close()
	if service.javbus != nil {
		service.javbus.Close()
	}
}

// ConfigureMetaTube loads the cached poster toggle and records the public
// MetaTube base URL used to build poster URLs. Call once at startup.
func (service *Service) ConfigureMetaTube(ctx context.Context, publicURL string) error {
	setting, _, err := database.LoadSetting[MetaTubePostersSetting](ctx, service.database, metatubePostersSetting)
	if err != nil {
		return fmt.Errorf("load MetaTube posters setting: %w", err)
	}
	service.metatubePostersMu.Lock()
	defer service.metatubePostersMu.Unlock()
	service.metatubePublicURL = publicURL
	service.metatubePosters = setting
	return nil
}

// MetaTubePosters returns the cached poster toggle.
func (service *Service) MetaTubePosters() MetaTubePostersSetting {
	service.metatubePostersMu.RLock()
	defer service.metatubePostersMu.RUnlock()
	return service.metatubePosters
}

// UpdateMetaTubePosters persists the toggle and refreshes the cache.
func (service *Service) UpdateMetaTubePosters(ctx context.Context, setting MetaTubePostersSetting) error {
	if err := database.SaveSetting(ctx, service.database, metatubePostersSetting, setting); err != nil {
		return fmt.Errorf("save MetaTube posters setting: %w", err)
	}
	service.metatubePostersMu.Lock()
	defer service.metatubePostersMu.Unlock()
	service.metatubePosters = setting
	return nil
}

// metatubePosterBaseURL returns the MetaTube base URL when posters are
// enabled, or "" when the feature is off.
func (service *Service) metatubePosterBaseURL() string {
	service.metatubePostersMu.RLock()
	defer service.metatubePostersMu.RUnlock()
	if !service.metatubePosters.Enabled || service.metatubePublicURL == "" {
		return ""
	}
	return service.metatubePublicURL
}

func (service *Service) Search(
	ctx context.Context,
	keyword string,
	options domain.SearchOptions,
) ([]Movie, error) {
	keyword = strings.TrimSpace(keyword)
	key := fmt.Sprintf("search:%q:%#v", keyword, options)
	movies, err := cachedJavDB(ctx, service, service.lists, key, func(ctx context.Context) ([]domain.Movie, error) {
		return service.javdb.Search(ctx, keyword, options)
	})
	if err != nil {
		return nil, fmt.Errorf("search JavDB: %w", err)
	}
	return projectMovies(ctx, movies, service.metatubePosterBaseURL()), nil
}

func (service *Service) Browse(
	ctx context.Context,
	options domain.BrowseOptions,
) ([]Movie, error) {
	key := fmt.Sprintf("browse:%#v", options)
	movies, err := cachedJavDB(ctx, service, service.lists, key, func(ctx context.Context) ([]domain.Movie, error) {
		return service.javdb.Browse(ctx, options)
	})
	if err != nil {
		return nil, fmt.Errorf("browse JavDB: %w", err)
	}
	return projectMovies(ctx, movies, service.metatubePosterBaseURL()), nil
}

func (service *Service) CatalogueDetail(ctx context.Context, movieID string) (domain.MovieDetail, error) {
	return cachedJavDB(ctx, service, service.details, movieID, func(ctx context.Context) (domain.MovieDetail, error) {
		detail, err := service.javdb.MovieDetail(ctx, movieID)
		if err != nil {
			return domain.MovieDetail{}, err
		}
		if err := service.completeMovieTags(ctx, &detail); err != nil {
			return domain.MovieDetail{}, err
		}
		return detail, nil
	})
}

func (service *Service) RefreshCatalogueDetail(ctx context.Context, movieID string) (domain.MovieDetail, error) {
	service.details.invalidate(movieID)
	return service.CatalogueDetail(ctx, movieID)
}

func (service *Service) MovieDetail(ctx context.Context, movieID string) (MovieDetail, error) {
	movie, err := service.CatalogueDetail(ctx, movieID)
	if err != nil {
		return MovieDetail{}, fmt.Errorf("get JavDB movie detail: %w", err)
	}
	projected := projectMovies(ctx, []domain.Movie{movie.Movie}, service.metatubePosterBaseURL())
	return MovieDetail{
		Movie:         projected[0],
		Zone:          movie.Zone,
		ActorMovies:   movie.ActorMovies,
		RelatedMovies: movie.RelatedMovies,
	}, nil
}

func (service *Service) MovieCode(ctx context.Context, movieID string) (string, error) {
	movie, err := service.CatalogueDetail(ctx, movieID)
	if err != nil {
		return "", err
	}
	return movie.Code, nil
}

func (service *Service) MovieSummary(ctx context.Context, movieID string) (domain.MovieSummary, error) {
	detail, err := service.MovieDetail(ctx, movieID)
	if err != nil {
		return domain.MovieSummary{}, err
	}
	return domain.MovieSummary{
		ID:          detail.ID,
		Code:        detail.Code,
		Title:       detail.Title,
		Cover:       detail.Cover,
		ReleaseDate: detail.ReleaseDate,
	}, nil
}

// BrowseMovies returns browsed movies as domain types for callers requiring domain boundaries.
func (service *Service) BrowseMovies(ctx context.Context, options domain.BrowseOptions) ([]domain.Movie, error) {
	movies, err := service.Browse(ctx, options)
	if err != nil {
		return nil, err
	}
	res := make([]domain.Movie, len(movies))
	for i, m := range movies {
		res[i] = m.Movie
	}
	return res, nil
}

func (service *Service) Media(ctx context.Context, rawURL string) (domain.Media, error) {
	media, err := service.javdb.FetchMedia(ctx, rawURL)
	if err != nil {
		return domain.Media{}, fmt.Errorf("fetch JavDB media: %w", err)
	}
	return media, nil
}

func (service *Service) Tags(ctx context.Context, zone domain.Zone) ([]domain.TagCategory, error) {
	categories, err := cachedJavDB(ctx, service, service.tags, string(zone), func(ctx context.Context) ([]domain.TagCategory, error) {
		return service.javdb.Tags(ctx, zone)
	})
	if err != nil {
		return nil, fmt.Errorf("get JavDB tags: %w", err)
	}
	return categories, nil
}

func (service *Service) ResolveMovieID(ctx context.Context, code string) (string, error) {
	id, err := service.javdb.ResolveMovieID(ctx, code)
	if err != nil {
		return "", fmt.Errorf("resolve JavDB movie ID: %w", err)
	}
	if err := service.persistActiveRoute(ctx); err != nil {
		return "", err
	}
	return id, nil
}

func projectMovies(
	ctx context.Context,
	source []domain.Movie,
	posterBaseURL string,
) []Movie {
	if len(source) == 0 {
		return []Movie{}
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	result := make([]Movie, len(source))
	for index, item := range source {
		releaseStatus := ReleaseUnknown
		item.ReleaseDate = strings.TrimSpace(item.ReleaseDate)
		if item.ReleaseDate != "" {
			releaseDate, err := time.ParseInLocation("2006-01-02", item.ReleaseDate, time.Local)
			if err != nil {
				slog.WarnContext(ctx, "invalid JavDB release date; omitting date",
					"movie_id", item.ID, "field", "release_date", "value", item.ReleaseDate)
				item.ReleaseDate = ""
			} else if releaseDate.After(today) {
				releaseStatus = ReleaseUpcoming
			} else {
				releaseStatus = ReleaseReleased
			}
		}
		if posterBaseURL != "" {
			// Poster URLs are constructed, never fetched, so list pages stay fast.
			item.Poster = metatubePosterURL(posterBaseURL, item.Code)
		}
		result[index] = Movie{
			Movie:         item,
			ReleaseStatus: releaseStatus,
		}
	}
	return result
}

// metatubePosterURL builds the MetaTube primary-image URL for a release
// code. Returns "" when the code cannot be mapped to a JAV321 ID.
func metatubePosterURL(publicURL, code string) string {
	id := jav321ID(code)
	if id == "" {
		return ""
	}
	return publicURL + "/v1/images/primary/JAV321/" + url.PathEscape(id) + "?ratio=-1&pos=-1&auto=False&quality=90"
}

// jav321ID converts a release code (e.g. SSIS-001) to a JAV321 provider ID
// (e.g. ssis00001): lowercase letters + digits zero-padded to 5.
// Returns "" for formats JAV321 cannot represent: FC2-XXXXXXX (the FC2
// prefix contains a digit), pure digits, missing letter prefix, or any
// code that is not letters followed by digits.
func jav321ID(code string) string {
	cleaned := strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if cleaned == "" {
		return ""
	}
	prefix, number := "", ""
	if parts := strings.Split(cleaned, "-"); len(parts) == 2 {
		prefix, number = parts[0], parts[1]
	} else if len(parts) == 1 {
		// No hyphen: split at the first digit.
		index := strings.IndexFunc(cleaned, unicode.IsDigit)
		if index < 0 {
			return ""
		}
		prefix, number = cleaned[:index], cleaned[index:]
	} else {
		return ""
	}
	if prefix == "" || number == "" {
		return ""
	}
	for _, r := range prefix {
		if !unicode.IsLetter(r) || r > unicode.MaxASCII {
			return ""
		}
	}
	for _, r := range number {
		if !unicode.IsDigit(r) {
			return ""
		}
	}
	padded := number
	for len(padded) < 5 {
		padded = "0" + padded
	}
	return strings.ToLower(prefix) + padded
}

func (service *Service) persistActiveRoute(ctx context.Context) error {
	service.persistRouteMu.Lock()
	defer service.persistRouteMu.Unlock()
	active, ok := service.javdb.Route()
	if !ok {
		return nil
	}
	route := persistedRoute{Host: active.Host, LatencyMS: active.Latency.Milliseconds()}
	if route == service.lastSavedRoute {
		return nil
	}
	if err := database.SaveSetting(ctx, service.database, javdbRouteSetting, route); err != nil {
		return fmt.Errorf("cache JavDB route: %w", err)
	}
	service.lastSavedRoute = route
	return nil
}
