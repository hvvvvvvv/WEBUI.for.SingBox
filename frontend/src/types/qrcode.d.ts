declare module 'qrcode' {
  interface QRCodeCanvasOptions {
    errorCorrectionLevel?: 'L' | 'M' | 'Q' | 'H'
    margin?: number
    width?: number
    color?: {
      dark?: string
      light?: string
    }
  }

  interface QRCodeAPI {
    toCanvas(canvas: HTMLCanvasElement, text: string, options?: QRCodeCanvasOptions): Promise<void>
  }

  const QRCode: QRCodeAPI
  export default QRCode
}
