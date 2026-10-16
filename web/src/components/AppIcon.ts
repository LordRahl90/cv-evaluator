// Inline stroke icons, copied from the approved mockups. Styled by
// src/styles/components/icon.css (.icon); size comes from the parent's CSS.
import { defineComponent, h, type PropType } from 'vue'

type Shape = ['path', { d: string }] | ['circle', { cx: number; cy: number; r: number }]

const ICONS = {
  home: [['path', { d: 'M3 10.5 12 3l9 7.5V20a1 1 0 0 1-1 1h-5v-6H9v6H4a1 1 0 0 1-1-1z' }]],
  search: [
    ['circle', { cx: 11, cy: 11, r: 7 }],
    ['path', { d: 'm20 20-3.5-3.5' }],
  ],
  star: [['path', { d: 'm12 3 2.7 5.6 6.1.9-4.4 4.3 1 6.1L12 17l-5.4 2.9 1-6.1L3.2 9.5l6.1-.9z' }]],
  file: [
    ['path', { d: 'M14 3H6a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8z' }],
    ['path', { d: 'M14 3v5h5M9 13h6M9 17h6' }],
  ],
  bolt: [['path', { d: 'M13 2 4 14h7l-1 8 9-12h-7z' }]],
  key: [
    ['circle', { cx: 8, cy: 15, r: 4 }],
    ['path', { d: 'm11 12 9-9M17 6l3 3M15 8l2 2' }],
  ],
  sliders: [
    ['path', { d: 'M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12' }],
    ['circle', { cx: 16, cy: 6, r: 2 }],
    ['circle', { cx: 10, cy: 12, r: 2 }],
    ['circle', { cx: 18, cy: 18, r: 2 }],
  ],
  check: [['path', { d: 'm5 12 5 5 9-10' }]],
  upload: [['path', { d: 'M12 16V4M7 9l5-5 5 5M4 20h16' }]],
  plus: [['path', { d: 'M12 5v14M5 12h14' }]],
  logout: [['path', { d: 'M15 4h3a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-3M10 17l-5-5 5-5M5 12h11' }]],
  menu: [['path', { d: 'M4 7h16M4 12h16M4 17h16' }]],
  close: [['path', { d: 'M6 6l12 12M18 6 6 18' }]],
  alert: [
    ['circle', { cx: 12, cy: 12, r: 9 }],
    ['path', { d: 'M12 8v5M12 16.5v.01' }],
  ],
  arrowLeft: [['path', { d: 'M19 12H5M11 6l-6 6 6 6' }]],
  refresh: [['path', { d: 'M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7' }]],
} satisfies Record<string, Shape[]>

export type IconName = keyof typeof ICONS

export default defineComponent({
  name: 'AppIcon',
  props: {
    name: { type: String as PropType<IconName>, required: true },
  },
  setup(props) {
    return () =>
      h(
        'svg',
        { class: 'icon', viewBox: '0 0 24 24', 'aria-hidden': 'true', focusable: 'false' },
        (ICONS[props.name] as Shape[]).map(([tag, attrs]) => h(tag, attrs)),
      )
  },
})
