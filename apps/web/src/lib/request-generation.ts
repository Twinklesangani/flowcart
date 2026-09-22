export function isCurrentGeneration(requestGeneration: number, currentGeneration: number) {
  return requestGeneration === currentGeneration;
}