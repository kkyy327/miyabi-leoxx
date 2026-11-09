import { MonitorCogIcon, MonitorIcon, MoonIcon, SunIcon } from 'lucide-react'
import { useTheme } from 'next-themes'

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useSettingsStore, type CoverStyle } from '@/stores/settings'
import { SettingRow, SettingsSection } from './shared'

const THEME_OPTIONS = [
  { value: 'system', icon: MonitorIcon },
  { value: 'light', icon: SunIcon },
  { value: 'dark', icon: MoonIcon }
]

export function AppearanceSection() {
  const { theme, setTheme } = useTheme()
  const coverStyle = useSettingsStore(state => state.coverStyle)
  const setCoverStyle = useSettingsStore(state => state.setCoverStyle)

  return (
    <SettingsSection icon={<MonitorCogIcon className="size-4" />} title="外观">
      <SettingRow title="主题" description="控制应用的明暗色主题" inline>
        <Tabs value={theme} onValueChange={setTheme}>
          <TabsList>
            {THEME_OPTIONS.map(option => (
              <TabsTrigger key={option.value} value={option.value}>
                <option.icon className="size-4" />
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </SettingRow>
      <SettingRow
        title="封面样式"
        description="竖图用海报、横图用宽屏封面；所选图缺失时自动用另一张补上"
        inline
      >
        <Tabs value={coverStyle} onValueChange={value => setCoverStyle(value as CoverStyle)}>
          <TabsList>
            <TabsTrigger value="poster">竖图</TabsTrigger>
            <TabsTrigger value="cover">横图</TabsTrigger>
          </TabsList>
        </Tabs>
      </SettingRow>
    </SettingsSection>
  )
}
