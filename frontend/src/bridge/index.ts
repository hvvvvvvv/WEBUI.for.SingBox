export { EventsOn, EventsOff, EventsEmit, initWebSocket } from './ws'
export * from './app'
export * from './rpc'
export { loadAuthToken } from './http'

// Browser clipboard/navigation helpers.
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

// Browser notification helpers.
export const IsNotificationAvailable = async () => 'Notification' in window
export const RequestNotificationAuthorization = async () => {
  if (!('Notification' in window)) return 'denied' as NotificationPermission
  if (Notification.permission !== 'default') return Notification.permission
  return await Notification.requestPermission()
}
