import { defineConfig } from 'vite'
import { idePlugin } from '@antennae-space/plugin-sdk/vite'

// web/src/index.tsx → web/index.js, one ES module as the IDE loads it.
export default defineConfig({ plugins: [idePlugin()] })
