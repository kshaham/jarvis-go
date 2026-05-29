import {
  forceCenter,
  forceLink,
  forceManyBody,
  forceSimulation,
  type SimulationLinkDatum,
  type SimulationNodeDatum,
} from 'd3-force';

interface LayoutNode extends SimulationNodeDatum {
  id: string;
}

export interface LayoutLink {
  source: string;
  target: string;
}

// settleLayout — run a d3-force simulation to a SETTLED state in one
// synchronous pass (off the render path), then hand back final
// positions. React Flow renders/pans/zooms those positions; we never
// re-tick per frame (that would cause a re-render storm). Force layout
// (not dagre/elk) because the agent message graph is undirected and
// cyclic — the organic constellation fits the data and the aesthetic.
export function settleLayout(
  nodeIds: string[],
  links: LayoutLink[],
  width: number,
  height: number,
  ticks = 220,
): Map<string, { x: number; y: number }> {
  const nodes: LayoutNode[] = nodeIds.map((id) => ({ id }));
  const linkData: SimulationLinkDatum<LayoutNode>[] = links.map((l) => ({
    source: l.source,
    target: l.target,
  }));
  const sim = forceSimulation<LayoutNode>(nodes)
    .force('charge', forceManyBody<LayoutNode>().strength(-420))
    .force(
      'link',
      forceLink<LayoutNode, SimulationLinkDatum<LayoutNode>>(linkData)
        .id((d) => d.id)
        .distance(130)
        .strength(0.35),
    )
    .force('center', forceCenter<LayoutNode>(width / 2, height / 2))
    .stop();
  sim.tick(ticks);
  const out = new Map<string, { x: number; y: number }>();
  for (const n of nodes) {
    out.set(n.id, { x: n.x ?? width / 2, y: n.y ?? height / 2 });
  }
  return out;
}
