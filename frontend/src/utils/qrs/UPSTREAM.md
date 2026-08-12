# QRS compatibility baseline

Verified on 2026-08-12.

- `SagerNet/sing-box` `testing`: `3e16ce764818796996c1be3f12b0c6008808cf34`
  - `experimental/libbox/profile_import.go`
- `SagerNet/sing-box-for-android` `main`: `0a401b69b63d5bc40be5c018baa117a04eeb26a1`
  - `qrs/QRSEncoder.kt`
  - `qrs/QRSDecoder.kt`
  - `qrs/LubyCodec.kt`
  - `qrs/QRSConstants.kt`
  - `qrs/SolitonDistribution.kt`
  - `qrs/ByteArrayExtensions.kt`
  - `compose/component/qr/QRSDialog.kt`

The generated profile uses a compatible complete gzip stream rather than requiring a
byte-for-byte match with Go's deflate implementation. QRS uses a zlib-wrapped deflate stream,
standard padded Base64, Kotlin's seeded XorWow generator, and the field ordering from the files
above.
