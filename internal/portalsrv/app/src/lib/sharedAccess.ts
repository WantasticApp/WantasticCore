import type { Peer } from "$store/peer";

export interface SharedPeerMeta {
  isShared: boolean;
  ownerName: string;
  viewerCanWrite: boolean;
}

export interface SharedResourceMetaSource {
  peer_id?: string;
  peer_ip?: string;
  is_shared?: boolean;
  owner_name?: string;
  viewer_can_write?: boolean;
}

export interface PeerSharedLookup {
  byId: Map<string, SharedPeerMeta>;
  byAssignedIp: Map<string, SharedPeerMeta>;
}

function normalizeIp(ip?: string): string {
  return (ip ?? "").replace("/32", "").trim();
}

export function createPeerSharedLookup(peers: Peer[]): PeerSharedLookup {
  const byId = new Map<string, SharedPeerMeta>();
  const byAssignedIp = new Map<string, SharedPeerMeta>();

  for (const peer of peers) {
    const isShared = peer.is_shared === true;
    const ownerName = peer.owner_name || "";
    const viewerCanWrite =
      peer.viewer_can_write ?? (isShared ? false : true);
    const meta = { isShared, ownerName, viewerCanWrite };

    byId.set(peer.id, meta);

    const assignedIp = normalizeIp(peer.assigned_ip || peer.router_ip);
    if (assignedIp) {
      byAssignedIp.set(assignedIp, meta);
    }
  }

  return { byId, byAssignedIp };
}

export function resolveSharedMeta(
  resource: SharedResourceMetaSource,
  lookup: PeerSharedLookup
): SharedPeerMeta {
  const peerMeta = resource.peer_id
    ? lookup.byId.get(resource.peer_id)
    : lookup.byAssignedIp.get(normalizeIp(resource.peer_ip));

  const isShared = peerMeta?.isShared ?? (resource.is_shared === true);
  const ownerName = peerMeta?.ownerName || resource.owner_name || "";
  const viewerCanWrite =
    peerMeta?.viewerCanWrite ??
    resource.viewer_can_write ??
    (isShared ? false : true);

  return { isShared, ownerName, viewerCanWrite };
}
