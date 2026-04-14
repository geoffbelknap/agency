/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_ENABLE_EXPERIMENTAL_SURFACES?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

declare const __BUILD_ID__: string;
declare const __BUILD_TIME__: string;
