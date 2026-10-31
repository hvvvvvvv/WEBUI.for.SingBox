export interface CoreRuntimePaths {
  appBasePath: string
  coreBasePath: string
}

// One pass matches Go strings.NewReplacer: replacement text is never expanded
// again, and $ characters in real directory names remain literal.
export const previewCoreRuntimeValue = (value: string, paths: CoreRuntimePaths): string =>
  value.replace(/\$APP_BASE_PATH|\$CORE_BASE_PATH/g, (key) =>
    key === '$APP_BASE_PATH' ? paths.appBasePath : paths.coreBasePath,
  )
