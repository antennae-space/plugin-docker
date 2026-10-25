// State of the Docker tool: status of docker, Compose stack of the project, containers of the
// host, instant statistics, selection. Lists are refreshed while the tool is shown.
import { createSignal } from 'solid-js'
import { api, errorToast } from './plugin'

export interface Port {
  ip?: string
  published?: number
  target: number
  protocol: string
}

export interface Container {
  id: string
  name: string
  image: string
  state: string
  status: string
  health?: string
  project?: string
  service?: string
  ports: Port[]
}

export interface Stack {
  name: string
  services: string[]
  containers: Container[]
}

export interface DockerStatus {
  available: boolean
  error?: string
  compose: boolean
  composeFile?: string
  profiles?: string[]
}

export interface Stats {
  cpu: string
  mem: string
  memPerc: string
  net: string
  block: string
  pids: string
}

/** The Stack row of the Project tab, or a container (by id). */
export type Selection = { kind: 'stack' } | { kind: 'container'; id: string } | null

const [status, setStatus] = createSignal<DockerStatus | null>(null)
const [stack, setStack] = createSignal<{ stack?: Stack; error?: string } | null>(null)
const [host, setHost] = createSignal<{ list?: Container[]; error?: string } | null>(null)
const [stats, setStats] = createSignal<Record<string, Stats>>({})
const [selected, setSelected] = createSignal<Selection>(null)
/** Keys of the actions running in the backend (`stack`, a service or a container id): spinners. */
const [busy, setBusy] = createSignal<Set<string>>(new Set())
export { status, stack, host, stats, selected, setSelected, busy }

/** The Compose profiles chosen in the tool, kept by the backend for the project (its assistant
 * tools use them too). */
const [chosen, setChosen] = createSignal<string[]>([])
export const chosenProfiles = chosen
export const profiles = () => chosen().filter((p) => status()?.profiles?.includes(p))

export async function loadProfiles() {
  try {
    setChosen((await api.rpc<{ profiles: string[] }>('profiles')).profiles)
  } catch {
    /* none chosen */
  }
}

export function toggleProfile(p: string) {
  const next = chosen().includes(p) ? chosen().filter((x) => x !== p) : [...chosen(), p]
  setChosen(next)
  api.rpc('setProfiles', { profiles: next }).catch(errorToast)
}

export async function refreshStatus() {
  try {
    setStatus(await api.rpc<DockerStatus>('status'))
  } catch (e) {
    setStatus({ available: false, compose: false, error: (e as Error).message })
  }
}

export async function refreshStack() {
  if (!status()?.available || !status()?.compose || !status()?.composeFile) return
  try {
    setStack({ stack: await api.rpc<Stack>('stack', { profiles: profiles() }) })
  } catch (e) {
    setStack({ error: (e as Error).message })
  }
}

export async function refreshHost() {
  if (!status()?.available) return
  try {
    setHost({ list: await api.rpc<Container[]>('containers') })
  } catch (e) {
    setHost({ error: (e as Error).message })
  }
}

/** Instant values of the running containers of the shown list. */
export async function refreshStats(list: Container[]) {
  const ids = list.filter((c) => c.state === 'running').map((c) => c.id)
  try {
    setStats(ids.length ? await api.rpc<Record<string, Stats>>('stats', { ids }) : {})
  } catch {
    /* the list shows the error */
  }
}

/** Container of the selection, in the stack or the host list. */
export function selectedContainer(): Container | undefined {
  const s = selected()
  if (s?.kind !== 'container') return undefined
  return stack()?.stack?.containers.find((c) => c.id === s.id) ?? host()?.list?.find((c) => c.id === s.id)
}

async function busyWhile(key: string, f: () => Promise<unknown>) {
  setBusy((b) => new Set(b).add(key))
  try {
    await f()
  } catch (e) {
    errorToast(e)
  } finally {
    setBusy((b) => {
      const n = new Set(b)
      n.delete(key)
      return n
    })
    void refreshStack()
    void refreshHost()
  }
}

/** Start, stop or restart of the stack (service empty) or of a service, run by the backend. */
export const composeAction = (action: 'start' | 'stop' | 'restart', service = '') =>
  busyWhile(service || 'stack', () => api.rpc('compose', { profiles: profiles(), action, service }))

export const containerAction = (action: 'start' | 'stop' | 'restart' | 'remove', id: string) =>
  busyWhile(id, () => api.rpc('container', { action, id }))

/** A longer Compose action (recreate, rebuild, pull, down) run by the backend in a console that
 * shows its progress. */
export async function composeTask(kind: 'recreate' | 'rebuild' | 'pull' | 'down' | 'downVolumes', service = '') {
  try {
    const r = await api.rpc<{ console: string }>('task', { profiles: profiles(), kind, service })
    ide.showConsole(r.console)
  } catch (e) {
    errorToast(e)
  }
}

/** A terminal in a container: bash when it has one, else sh. */
export async function shell(c: Container, root = false) {
  try {
    const r = await api.rpc<{ console: string }>('shell', { id: c.id, root, title: c.service || c.name })
    ide.showConsole(r.console)
  } catch (e) {
    errorToast(e)
  }
}

export function portLabel(p: Port) {
  if (!p.published) return `${p.target}/${p.protocol}`
  return `${p.ip ? p.ip + ':' : ''}${p.published}→${p.target}${p.protocol === 'tcp' ? '' : '/' + p.protocol}`
}
