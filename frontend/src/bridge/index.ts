export { EventsOn, EventsOff, EventsEmit, initWebSocket } from './ws'
export * from './io'
export * from './net'
export * from './exec'
export * from './app'
export * from './server'
export * from './mmdb'
export {
  AUTH_REQUIRED_EVENT,
  setAuthToken,
  getAuthToken,
  loadAuthToken,
  clearAuthToken,
} from './http'

// Stubs for Wails window functions (no-op in C/S mode)
export const WindowHide = () => {}
export const WindowShow = () => {}
export const WindowReloadApp = () => location.reload()
export const WindowSetSystemDefaultTheme = () => {}
export const WindowIsMaximised = async () => false
export const WindowIsMinimised = async () => false
export const WindowSetSize = (_w: number, _h: number) => {}
export const WindowCenter = () => {}
export const WindowFullscreen = () => {}
export const WindowToggleMaximise = () => {}
export const WindowMinimise = () => {}
export const WindowSetAlwaysOnTop = (_onTop: boolean) => {}

// Stubs for Wails clipboard/browser functions
export const ClipboardSetText = async (text: string): Promise<boolean> => {
  await navigator.clipboard.writeText(text)
  return true
}
export const ClipboardGetText = async () => {
  return navigator.clipboard.readText()
}
export const BrowserOpenURL = (url: string) => {
  window.open(url, '_blank')
}

// Stubs for Wails notification functions
export const IsNotificationAvailable = async () => 'Notification' in window
export const RequestNotificationAuthorization = async () => {
  if ('Notification' in window) {
    await Notification.requestPermission()
  }
}
