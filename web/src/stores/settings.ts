import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export type CoverStyle = 'poster' | 'cover'

type SettingsState = {
  nsfwMode: boolean
  setNsfwMode: (enabled: boolean) => void
  coverStyle: CoverStyle
  setCoverStyle: (style: CoverStyle) => void
}

export const useSettingsStore = create<SettingsState>()(
  persist(
    set => ({
      nsfwMode: true,
      setNsfwMode: nsfwMode => set({ nsfwMode }),
      coverStyle: 'poster',
      setCoverStyle: coverStyle => set({ coverStyle })
    }),
    {
      name: 'miyabi-settings'
    }
  )
)
