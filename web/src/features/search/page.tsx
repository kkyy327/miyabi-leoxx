import { SearchIcon, XIcon } from 'lucide-react'
import { type FormEvent, useState } from 'react'

import { useDiscoverMovies, useSearchMovies } from '@/api/discover'
import { AppPage } from '@/components/app-page'
import { ErrorState } from '@/components/error-state'
import { MovieGridSkeleton } from '@/components/movie'
import { PageHeader } from '@/components/page-header'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput
} from '@/components/ui/input-group'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { DISCOVER_PAGE_SIZE } from '@/features/discover/constants'
import { MovieGrid } from '@/features/discover/movie-grid'
import { DiscoverResults } from '@/features/discover/results'

const SEARCH_SORT_OPTIONS = [
  { value: 'release', label: '最新发行' },
  { value: 'hit', label: '热度' },
  { value: 'score', label: '评分' }
]

export function SearchPage({
  keyword,
  page,
  onSearch,
  onPageChange
}: {
  keyword: string
  page: number
  onSearch: (keyword: string) => void
  onPageChange: (page: number) => void
}) {
  return (
    <AppPage>
      <PageHeader title="搜索" description="搜索 JavDB 的所有影片" />
      <SearchForm key={`form:${keyword}`} keyword={keyword} onSearch={onSearch} />
      {keyword ? (
        <SearchResults
          key={`results:${keyword}`}
          keyword={keyword}
          page={page}
          onPageChange={onPageChange}
        />
      ) : (
        <SearchRankings />
      )}
    </AppPage>
  )
}

function SearchForm({
  keyword,
  onSearch
}: {
  keyword: string
  onSearch: (keyword: string) => void
}) {
  const [draft, setDraft] = useState(keyword)

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    onSearch(draft.trim())
  }

  return (
    <form className="w-full max-w-xl" onSubmit={submit}>
      <InputGroup>
        <InputGroupAddon>
          <InputGroupButton type="submit" size="icon-xs">
            <SearchIcon className="size-4" />
          </InputGroupButton>
        </InputGroupAddon>
        <InputGroupInput
          type="text"
          inputMode="search"
          enterKeyHint="search"
          value={draft}
          onChange={event => setDraft(event.target.value)}
          placeholder="搜索番号、标题或演员"
        />
        {draft ? (
          <InputGroupAddon align="inline-end">
            <InputGroupButton
              size="icon-xs"
              onClick={() => {
                setDraft('')
                onSearch('')
              }}
            >
              <XIcon />
            </InputGroupButton>
          </InputGroupAddon>
        ) : null}
      </InputGroup>
    </form>
  )
}

function SearchRankings() {
  return (
    <div className="flex flex-col gap-8">
      <RankingSection title="热门影片" sort="hit" />
      <RankingSection title="最佳榜单" sort="score" />
    </div>
  )
}

function RankingSection({ title, sort }: { title: string; sort: string }) {
  const movies = useDiscoverMovies({ sort, order: 'desc', page: 1, limit: 10 })
  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-xl font-semibold tracking-normal">{title}</h2>
      {movies.isError ? (
        <ErrorState
          message="数据加载失败，请检查网络或代理设置"
          onRetry={() => movies.refetch()}
          retrying={movies.isFetching}
        />
      ) : movies.isPending || !movies.data ? (
        <MovieGridSkeleton count={10} />
      ) : movies.data.length === 0 ? null : (
        <MovieGrid movies={movies.data} />
      )}
    </section>
  )
}

function SearchResults({
  keyword,
  page,
  onPageChange
}: {
  keyword: string
  page: number
  onPageChange: (page: number) => void
}) {
  const [sort, setSort] = useState('release')
  const movies = useSearchMovies({ query: keyword, sort, page, limit: DISCOVER_PAGE_SIZE })
  // 最新发行时按发行日期从新到旧排序 (搜女优时最新作品排前面; 无日期的放最后)
  // 热度/评分排序直接用 JavDB 返回的顺序
  const sorted =
    sort === 'release' && movies.data
      ? [...movies.data].sort((a, b) => {
          const da = a.release_date || ''
          const db = b.release_date || ''
          if (da === db) return 0
          if (!da) return 1
          if (!db) return -1
          return db.localeCompare(da)
        })
      : movies.data
  return (
    <div className="flex flex-col gap-4">
      <div className="flex justify-end">
        <Select value={sort} onValueChange={value => setSort(value)}>
          <SelectTrigger className="w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent position="popper" align="end">
            <SelectGroup>
              {SEARCH_SORT_OPTIONS.map(option => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>
      <DiscoverResults
        movies={sorted}
        loading={movies.isPending}
        fetching={movies.isFetching}
        error={movies.isError}
        searching
        page={page}
        onPageChange={onPageChange}
        onRetry={() => movies.refetch()}
      />
    </div>
  )
}

