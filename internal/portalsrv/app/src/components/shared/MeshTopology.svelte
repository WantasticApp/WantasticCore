<script lang="ts">
    import type { MeshNode } from "$store/peer";
    export let node: MeshNode;
    export let depth = 0;
</script>

<div class="mesh-node py-1">
    <div class="flex items-center gap-2 text-sm">
        <div
            class="w-2 h-2 rounded-full"
            class:bg-green-500={node.signal > -80}
            class:bg-yellow-500={node.signal <= -80 && node.signal > -90}
            class:bg-red-500={node.signal <= -90}
        ></div>
        <span class="font-bold text-gray-200">{node.name || "Unknown"}</span>
        <span class="text-xs text-gray-500 font-mono">{node.mac}</span>
        {#if node.ip}<span class="text-xs text-blue-400 font-mono"
                >{node.ip}</span
            >{/if}
        <span class="text-xs bg-gray-800 px-1 rounded text-gray-400"
            >{node.role}</span
        >
        <span class="text-xs text-gray-400">{node.signal} dBm</span>
    </div>
    {#if node.children && node.children.length > 0}
        <div class="ml-3 pl-3 border-l border-gray-700 mt-1">
            {#each node.children as child}
                <svelte:self node={child} depth={depth + 1} />
            {/each}
        </div>
    {/if}
</div>
