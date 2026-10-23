import { Graph, layout } from '@dagrejs/dagre'
import type { Node, Edge } from '@vue-flow/core'

export type FlowLayoutOptions = {
  direction?: 'TB' | 'LR'
  nodeWidth?: number
  nodeHeight?: number
  nodesep?: number
  ranksep?: number
  marginx?: number
  marginy?: number
}

const DEFAULT_NODE_WIDTH = 220
const DEFAULT_NODE_HEIGHT = 100

/**
 * Auto-layout Vue Flow nodes with dagre (top→bottom by default).
 * Mutates `node.position` in place and returns the same array.
 */
export function layoutFlowNodes(
  nodes: Node[],
  edges: Edge[],
  options: FlowLayoutOptions = {},
): Node[] {
  if (nodes.length === 0) return nodes

  const {
    direction = 'TB',
    nodeWidth = DEFAULT_NODE_WIDTH,
    nodeHeight = DEFAULT_NODE_HEIGHT,
    nodesep = 50,
    ranksep = 80,
    marginx = 20,
    marginy = 20,
  } = options

  const g = new Graph()
  g.setDefaultEdgeLabel(() => ({}))
  g.setGraph({ rankdir: direction, nodesep, ranksep, marginx, marginy })

  for (const node of nodes) {
    const measured = node as Node & {
      dimensions?: { width: number; height: number }
      measured?: { width: number; height: number }
    }
    const width = measured.dimensions?.width || measured.measured?.width || nodeWidth
    const height = measured.dimensions?.height || measured.measured?.height || nodeHeight
    g.setNode(node.id, { width, height })
  }

  const seen = new Set<string>()
  for (const edge of edges) {
    if (!edge.source || !edge.target) continue
    const key = `${edge.source}→${edge.target}`
    if (seen.has(key)) continue
    seen.add(key)
    if (!g.hasNode(edge.source) || !g.hasNode(edge.target)) continue
    g.setEdge(edge.source, edge.target)
  }

  layout(g)

  for (const node of nodes) {
    const pos = g.node(node.id)
    if (!pos) continue
    node.position = {
      x: pos.x - pos.width / 2,
      y: pos.y - pos.height / 2,
    }
  }

  return nodes
}
