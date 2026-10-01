/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Set to "" by the container build so API calls go to the same origin behind the proxy. */
  readonly VITE_API_BASE?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
