import '@testing-library/jest-dom/vitest'

// O xterm sonda o canvas ao ser importado; o jsdom não implementa getContext.
HTMLCanvasElement.prototype.getContext = (() => null) as unknown as typeof HTMLCanvasElement.prototype.getContext
