// lowlight 3.3.0 only exports its barrel, which eagerly references all/common
// grammars. Keep the pinned core path isolated; revalidate on package upgrades.
// Vite and Vitest alias exactly `lowlight` here, including rehype-highlight's
// import. Every caller must supply its explicit language registry.
export { createLowlight } from '../../../node_modules/lowlight/lib/index.js'
export const common = {}
