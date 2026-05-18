<script lang="ts">
  import { onMount, onDestroy } from "svelte";

  let canvas: HTMLCanvasElement;
  let ctx: CanvasRenderingContext2D | null;
  let animationFrameId: number;
  let width: number;
  let height: number;

  // Configuration
  const NODE_COUNT = 60;
  const CONNECTION_DISTANCE = 150;
  const PULSE_SPEED = 2;
  const NEON_CYAN = "#06b6d4";
  const NEON_MAGENTA = "#d946ef";

  interface Node {
    x: number;
    y: number;
    vx: number;
    vy: number;
  }

  interface Pulse {
    startNode: Node;
    endNode: Node;
    progress: number; // 0 to 1
    color: string;
  }

  let nodes: Node[] = [];
  let pulses: Pulse[] = [];

  function resize() {
    if (!canvas) return;
    const parent = canvas.parentElement;
    if (parent) {
      width = parent.clientWidth;
      height = parent.clientHeight;
      canvas.width = width;
      canvas.height = height;
    }
  }

  function initNodes() {
    nodes = [];
    for (let i = 0; i < NODE_COUNT; i++) {
      nodes.push({
        x: Math.random() * width,
        y: Math.random() * height,
        vx: (Math.random() - 0.5) * 0.5,
        vy: (Math.random() - 0.5) * 0.5,
      });
    }
  }

  function draw() {
    if (!ctx) return;
    ctx.clearRect(0, 0, width, height);

    // Update nodes
    nodes.forEach((node) => {
      node.x += node.vx;
      node.y += node.vy;

      // Bounce off walls
      if (node.x < 0 || node.x > width) node.vx *= -1;
      if (node.y < 0 || node.y > height) node.vy *= -1;
    });

    // Draw connections and spawn pulses
    ctx.lineWidth = 1;
    for (let i = 0; i < nodes.length; i++) {
      for (let j = i + 1; j < nodes.length; j++) {
        const dx = nodes[i].x - nodes[j].x;
        const dy = nodes[i].y - nodes[j].y;
        const dist = Math.sqrt(dx * dx + dy * dy);

        if (dist < CONNECTION_DISTANCE) {
          // Opacity based on distance
          const opacity = 1 - dist / CONNECTION_DISTANCE;
          ctx.strokeStyle = `rgba(100, 116, 139, ${opacity * 0.2})`; // Slate-500 equivalent, faint
          ctx.beginPath();
          ctx.moveTo(nodes[i].x, nodes[i].y);
          ctx.lineTo(nodes[j].x, nodes[j].y);
          ctx.stroke();

          // Randomly spawn a pulse
          if (Math.random() < 0.0005) {
            pulses.push({
              startNode: nodes[i],
              endNode: nodes[j],
              progress: 0,
              color: Math.random() > 0.5 ? NEON_CYAN : NEON_MAGENTA,
            });
          }
        }
      }
    }

    // Update and draw pulses
    for (let i = pulses.length - 1; i >= 0; i--) {
      const p = pulses[i];
      p.progress += PULSE_SPEED / 100;

      if (p.progress >= 1) {
        pulses.splice(i, 1);
        continue;
      }

      const currentX =
        p.startNode.x + (p.endNode.x - p.startNode.x) * p.progress;
      const currentY =
        p.startNode.y + (p.endNode.y - p.startNode.y) * p.progress;

      ctx.fillStyle = p.color;
      ctx.shadowBlur = 4;
      ctx.shadowColor = p.color;
      ctx.beginPath();
      ctx.arc(currentX, currentY, 2, 0, Math.PI * 2);
      ctx.fill();
      ctx.shadowBlur = 0; // Reset shadow
    }

    // Draw nodes
    ctx.fillStyle = "rgba(100, 116, 139, 0.4)";
    nodes.forEach((node) => {
      ctx.beginPath();
      ctx.arc(node.x, node.y, 2, 0, Math.PI * 2);
      ctx.fill();
    });

    animationFrameId = requestAnimationFrame(draw);
  }

  onMount(() => {
    ctx = canvas.getContext("2d");
    resize();
    initNodes();
    draw();

    window.addEventListener("resize", resize);
  });

  onDestroy(() => {
    if (typeof window !== "undefined") {
      window.removeEventListener("resize", resize);
      cancelAnimationFrame(animationFrameId);
    }
  });
</script>

<canvas bind:this={canvas} class="network-bg" />

<style>
  .network-bg {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    z-index: 0;
    pointer-events: none;
    opacity: 0.6; /* Adjust for subtlety */
  }

  /* Only show in light mode (default/no preference usually assumes light, 
     but we want to be explicit or follow parent preferences) 
     The parent component should control visibility via @media or classes 
  */
</style>
