/** Artifact slots follow transcript time even when the reader reverses the visual order. */
export function newestImageTiles<T extends { dataset: { artifactSlot?: string } }>(tiles: readonly T[]): T[] {
  return [...tiles].sort((a, b) => Number(b.dataset.artifactSlot) - Number(a.dataset.artifactSlot))
}
