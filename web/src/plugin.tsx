// The Docker tool's door to the IDE: the sandbox API (`ide`) and the SDK's kit, under the names
// its components use (api.rpc, t, contextMenu, prompt, confirm…). Its methods are docker.* of its
// backend; nothing else of the IDE is reached.
import { createSignal, type JSX } from 'solid-js'
import { Icon as KitIcon, EmptyState as KitEmpty, openMenu, type IconName } from '@antennae-space/plugin-sdk/ui'

export const api = {
  rpc: <T = any>(method: string, params?: unknown) => ide.rpc<T>(`docker.${method}`, params),
  /** An event of the backend; the function returned stops listening to it. */
  on: (event: string, f: (data: any) => void) => {
    let live = true
    ide.on(`docker.${event}`, (d) => live && f(d))
    return () => void (live = false)
  },
}

export const t = (text: string, params?: Record<string, unknown>) => ide.t(text, params)

/** What the tool shows: its tab, the tab of the detail pane (the profiles are the backend's). */
export interface DockerView {
  tab: 'project' | 'host' | 'disk'
  detail: 'infos' | 'logs'
}
const [view, setView] = createSignal<DockerView>({ tab: 'project', detail: 'infos' })
export const desk = {
  get: view,
  set: (f: (s: DockerView) => void) => setView((v) => {
    const n = { ...v }
    f(n)
    return n
  }),
}

export const Icon = (p: { name: string; size?: number; title?: string }) => <KitIcon name={p.name as IconName} size={p.size} title={p.title} />
export const EmptyState = (p: { icon?: string; text: string }) => <KitEmpty icon={(p.icon === 'docker' ? 'info' : p.icon) as IconName} text={p.text} />

export interface MenuItem {
  label: string
  action?: () => void
  danger?: boolean
  disabled?: boolean
  checked?: boolean
  separator?: boolean
}

/** A menu at the pointer: the kit's, with the core's item shape. */
export function contextMenu(e: MouseEvent, items: MenuItem[]) {
  e.preventDefault()
  e.stopPropagation()
  openMenu(
    items.map((it) => (it.separator ? 'separator' : { label: (it.checked ? '✓ ' : '') + it.label, run: () => it.action?.(), danger: it.danger, disabled: it.disabled })),
    e,
  )
}

export const toast = (text: string, kind?: 'info' | 'warn' | 'error' | 'ok') => ide.toast(text, kind)
export const errorToast = (e: unknown) => ide.toast(e instanceof Error ? e.message : String(e), 'error')

/** Asks yes or no in a dialog of the IDE, centered in its window. */
export const confirm = (text: string, danger = false) => ide.confirm({ title: t('Docker'), text, ok: danger ? t('Remove') : t('OK'), danger })

/** Asks a text in a dialog of the IDE: null when cancelled. */
export const prompt = (o: { title: string; label?: string }): Promise<string | null> => ide.prompt(o)

export type { JSX }
