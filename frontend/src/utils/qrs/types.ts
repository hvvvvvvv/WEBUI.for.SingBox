export const QRS_URL_PREFIX = 'https://qrss.netlify.app/#'

export const QRS_DEFAULT_SLICE_SIZE = 500
export const QRS_MIN_SLICE_SIZE = 100
export const QRS_MAX_SLICE_SIZE = 1500
export const QRS_RECOVERY_FACTOR = 1.3

export const QRS_DEFAULT_FPS = 10
export const QRS_MIN_FPS = 1
export const QRS_MAX_FPS = 60

export enum ProfileType {
  Local = 0,
  ICloud = 1,
  Remote = 2,
}

interface ProfileContentBase {
  name: string
  config: string
}

export interface LocalProfileContent extends ProfileContentBase {
  type: ProfileType.Local
}

export interface ICloudProfileContent extends ProfileContentBase {
  type: ProfileType.ICloud
  remotePath: string
}

export interface RemoteProfileContent extends ProfileContentBase {
  type: ProfileType.Remote
  remotePath: string
  autoUpdate: boolean
  autoUpdateInterval: number
  lastUpdated: bigint
}

export type ProfileContentInput = LocalProfileContent | ICloudProfileContent | RemoteProfileContent

export interface QRSEncodeOptions {
  sliceSize?: number
  frameCount?: number
}

export interface QRSFrame {
  base64: string
  content: string
  frameIndex: number
  totalBlocks: number
}

export interface QRSBundle {
  bpfData: Uint8Array
  wrappedData: Uint8Array
  frames: QRSFrame[]
  frameCount: number
  totalBlocks: number
}
