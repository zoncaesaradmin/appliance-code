import { describe, expect, it } from "vitest";
import { pickSpatialNeighbor, type NavRect } from "./spatialNav";

function tile(id: string, x: number, y: number): NavRect {
  return { id, x, y, width: 100, height: 80 };
}

describe("spatial navigation", () => {
  const grid = [
    tile("a", 0, 0),
    tile("b", 120, 0),
    tile("c", 240, 0),
    tile("d", 0, 100),
    tile("e", 120, 100)
  ];

  it("moves to the nearest neighbor in each direction", () => {
    expect(pickSpatialNeighbor(grid[0], grid, "right")?.id).toBe("b");
    expect(pickSpatialNeighbor(grid[1], grid, "left")?.id).toBe("a");
    expect(pickSpatialNeighbor(grid[1], grid, "down")?.id).toBe("e");
    expect(pickSpatialNeighbor(grid[3], grid, "up")?.id).toBe("a");
  });

  it("prefers the same row over a distant diagonal", () => {
    expect(pickSpatialNeighbor(grid[0], grid, "right")?.id).toBe("b");
    expect(pickSpatialNeighbor(grid[0], grid, "down")?.id).toBe("d");
  });

  it("returns undefined at the edge of the graph", () => {
    expect(pickSpatialNeighbor(grid[0], grid, "left")).toBeUndefined();
    expect(pickSpatialNeighbor(grid[0], grid, "up")).toBeUndefined();
  });
});
