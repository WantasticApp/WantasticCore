# Wantastic RouterOS Library v1.1
# Compatible: RouterOS v7.1+
#
# Usage:
#   /tool fetch url="https://get.wantastic.app/init.rsc" mode=https
#   /import init.rsc
#   :global wantasticDeploy
#   $wantasticDeploy privateKey=... publicKey=... endpoint=... address=... ...
#
# Coding rules (proven the hard way — see memory):
#   - Cleanup ops use :do{} on-error={} so "not found" is silent
#   - Critical ops are NOT wrapped — RouterOS prints its own error and halts
#   - Each critical step has a :put "[deploy] step=X ..." line before it so the
#     last printed step before the halt tells us where it failed
#   - Named function params are read with $name; cannot :set them. To get a
#     mutable copy, do :local mutableX $x at the top of the function body.
#   - Strings containing / = + are passed via :local then ($var) at call sites.

:global wantasticDeploy do={
  :local tag "WANTASTIC-WG"

  :put "[deploy] step=start"

  # check RouterOS version (critical — if WireGuard is unsupported, the first critical step below will fail but this check gives a clearer error)
  :local isUperVerion ([:pick [/system resource get version] 0] >= 7);
  :if (!$isUperVerion) do={:error "[deploy] FAIL: RouterOS version $verStr is too old; v7+ required"}
  :put ("[deploy] step=check-version ok version=" . [/system resource get version])

  # Check internet connectivity by resolving the endpoint host. This is not strictly required for deployment, but if it fails, the tunnel won't come up and the user might be confused why. If the endpoint is a domain name and this check fails, it's likely a DNS issue that also prevents the tunnel from working, so we might as well catch it here.
  :local epHost [:pick $endpoint 0 [:find $endpoint ":" 0]]
  :if ([:len $epHost] = 0) do={:error "[deploy] FAIL: endpoint must be host:port"}
  :local resolved [:resolve $epHost]
  :if ([:len $resolved] = 0) do={:error "[deploy] FAIL: cannot resolve endpoint host $epHost; check DNS"}
  :put ("[deploy] step=check-connectivity ok resolved-endpoint=" . $resolved)


  # Validate required params (using :error halts with a clear message)
  :if ([:len $privateKey] = 0) do={:error "[deploy] FAIL: privateKey required"}
  :if ([:len $publicKey]  = 0) do={:error "[deploy] FAIL: publicKey required"}
  :if ([:len $endpoint]   = 0) do={:error "[deploy] FAIL: endpoint required"}
  :if ([:len $address]    = 0) do={:error "[deploy] FAIL: address required"}
  :put "[deploy] step=validate ok"

  # Parse endpoint host:port
  :local colonPos [:find $endpoint ":" 0]
  :if ([:typeof $colonPos] = "nil") do={:error "[deploy] FAIL: endpoint must be host:port"}
  :local epHost [:pick $endpoint 0 $colonPos]
  :local epPort [:pick $endpoint ($colonPos + 1) [:len $endpoint]]
  :put ("[deploy] step=parse-endpoint host=" . $epHost . " port=" . $epPort)

  # Cleanup old config — on-error swallows "no such item". Order matters:
  # references (firewall rules, routes, addresses, peers) before the interface.
  :do {
    :foreach r in=[/ip/firewall/filter find where comment=$tag] do={/ip/firewall/filter remove $r}
    :foreach r in=[/ip/firewall/nat    find where comment=$tag] do={/ip/firewall/nat    remove $r}
    :foreach r in=[/ip/route           find where comment=$tag] do={/ip/route           remove $r}
    :foreach a in=[/ip/address         find where comment=$tag] do={/ip/address         remove $a}
    :foreach p in=[/interface/wireguard/peers find where interface="wg-wantastic"] do={/interface/wireguard/peers remove $p}
    /interface/wireguard remove [find where name="wg-wantastic"]
  } on-error={}
  :put "[deploy] step=cleanup ok"

  # Critical: WireGuard interface. Not wrapped — if WireGuard package is
  # missing or another interface owns the name, RouterOS prints the error
  # right after this :put and the script halts there.
  :put "[deploy] step=create-wg-iface attempting"
  /interface/wireguard add name=wg-wantastic listen-port=51820 mtu=$mtu private-key=$privateKey comment=$tag
  :put "[deploy] step=create-wg-iface ok"

  :put "[deploy] step=create-wg-peer attempting"
  /interface/wireguard/peers add interface=wg-wantastic public-key=$publicKey endpoint-address=$epHost endpoint-port=$epPort allowed-address=$allowedIPs persistent-keepalive=$keepalive comment=$tag
  :put "[deploy] step=create-wg-peer ok"

  :put "[deploy] step=add-address attempting"
  /ip/address add address=$address interface=wg-wantastic comment=$tag
  :put "[deploy] step=add-address ok"

  # Routes for allowed networks (each route may already exist if cleanup
  # missed it under a different comment — wrap individually)
  :foreach net in=[:toarray $allowedIPs] do={
    :if ($net != $address) do={
      :do {/ip/route add dst-address=$net gateway=wg-wantastic comment=$tag} on-error={}
    }
  }
  :put "[deploy] step=routes ok"

  # Required firewall rules — check first, install if missing, skip if present.
  # Each rule is matched by functional shape (chain + match + action), not by
  # comment, so an existing equivalent rule under any comment is honoured and
  # we don't create a duplicate. Cleanup above removed our own tagged rules,
  # so any survivor here belongs to the user.
  #
  # Placement must be SAFE. We want our rules at the top of their respective
  # chains so they cannot be shadowed by later accept/drop logic, but
  # `place-before=0` is not safe on fresh routers because an empty chain/table
  # has no item 0 and RouterOS halts with "no such item". So we first capture
  # the first existing rule in each target chain and only use `place-before`
  # when that anchor actually exists; otherwise we add the first rule plain.
  #
  #   1. input UDP 51820 — controller can complete the WireGuard handshake
  #   2. input from wg-wantastic — controller can reach router services (SSH,
  #      Winbox, API, web) over the tunnel
  #   3. forward in/out wg-wantastic — tunnelled traffic can transit
  #   4. nat srcnat masquerade — outgoing tunnel packets get a usable src IP
  :put "[deploy] step=firewall checking"
  :local inputAnchor ""
  :foreach r in=[/ip/firewall/filter find where chain=input] do={
    :set inputAnchor $r
  }
  :local forwardAnchor ""
  :foreach r in=[/ip/firewall/filter find where chain=forward] do={
    :set forwardAnchor $r
  }
  :local srcnatAnchor ""
  :foreach r in=[/ip/firewall/nat find where chain=srcnat] do={
    :set srcnatAnchor $r
  }

  :if ([:len [/ip/firewall/filter find where chain=input protocol=udp dst-port=51820 action=accept]] = 0) do={
    :if ([:len $inputAnchor] > 0) do={
      /ip/firewall/filter add chain=input protocol=udp dst-port=51820 action=accept place-before=$inputAnchor comment=$tag
    } else={
      /ip/firewall/filter add chain=input protocol=udp dst-port=51820 action=accept comment=$tag
    }
    :put "[deploy] step=firewall input-udp51820 installed"
  } else={:put "[deploy] step=firewall input-udp51820 skipped (exists)"}

  :if ([:len [/ip/firewall/filter find where chain=input in-interface=wg-wantastic action=accept]] = 0) do={
    :if ([:len $inputAnchor] > 0) do={
      /ip/firewall/filter add chain=input in-interface=wg-wantastic action=accept place-before=$inputAnchor comment=$tag
    } else={
      /ip/firewall/filter add chain=input in-interface=wg-wantastic action=accept comment=$tag
    }
    :put "[deploy] step=firewall input-wg installed"
  } else={:put "[deploy] step=firewall input-wg skipped (exists)"}

  :if ([:len [/ip/firewall/filter find where chain=forward in-interface=wg-wantastic action=accept]] = 0) do={
    :if ([:len $forwardAnchor] > 0) do={
      /ip/firewall/filter add chain=forward in-interface=wg-wantastic action=accept place-before=$forwardAnchor comment=$tag
    } else={
      /ip/firewall/filter add chain=forward in-interface=wg-wantastic action=accept comment=$tag
    }
    :put "[deploy] step=firewall forward-in-wg installed"
  } else={:put "[deploy] step=firewall forward-in-wg skipped (exists)"}

  :if ([:len [/ip/firewall/filter find where chain=forward out-interface=wg-wantastic action=accept]] = 0) do={
    :if ([:len $forwardAnchor] > 0) do={
      /ip/firewall/filter add chain=forward out-interface=wg-wantastic action=accept place-before=$forwardAnchor comment=$tag
    } else={
      /ip/firewall/filter add chain=forward out-interface=wg-wantastic action=accept comment=$tag
    }
    :put "[deploy] step=firewall forward-out-wg installed"
  } else={:put "[deploy] step=firewall forward-out-wg skipped (exists)"}

  :if ([:len [/ip/firewall/nat find where chain=srcnat out-interface=wg-wantastic action=masquerade]] = 0) do={
    :if ([:len $srcnatAnchor] > 0) do={
      /ip/firewall/nat add chain=srcnat out-interface=wg-wantastic action=masquerade place-before=$srcnatAnchor comment=$tag
    } else={
      /ip/firewall/nat add chain=srcnat out-interface=wg-wantastic action=masquerade comment=$tag
    }
    :put "[deploy] step=firewall srcnat-wg installed"
  } else={:put "[deploy] step=firewall srcnat-wg skipped (exists)"}

  :put "[deploy] step=firewall ok"

  # Save config globals for recovery (manual: run $wantasticRestore later)
  :global wantasticCfg ("$privateKey|$publicKey|$epHost:$epPort|$address|$allowedIPs|$mtu|$keepalive")
  :if ([:len $backupToken] > 0) do={:global wantasticToken $backupToken}
  :if ([:len $peerName]    > 0) do={:global wantasticName  $peerName}
  :if ([:len $hookURL]     > 0) do={:global wantasticHookURL $hookURL}
  :put "[deploy] step=save-globals ok"

  :put "[deploy] step=done — configuration applied"
  :log info "WANTASTIC-WG: deployed successfully"
}

:global wantasticBackup do={
  :global wantasticToken
  :global wantasticName
  :global wantasticHookURL

  :put "[backup] step=start"

  :if ([:len $wantasticToken] = 0) do={
    :put "[backup] FAIL: no backup token (deploy was not called with backupToken=)"
    :return false
  }
  :put ("[backup] step=token len=" . [:len $wantasticToken])

  # Defaults & overrides — :local then :set against the local (allowed)
  :local bName "mikrotik"
  :if ([:len $wantasticName] > 0) do={:set bName $wantasticName}
  :put ("[backup] step=name value=" . $bName)

  :local baseURL "https://console.wantastic.app/hooks/backup"
  :if ([:len $wantasticHookURL] > 0) do={:set baseURL $wantasticHookURL}
  :put ("[backup] step=baseURL value=" . $baseURL)

  # Cleanup any prior export file. on-error={} swallows "no such item".
  :do {/file remove [find where name~"wantastic-export"]} on-error={}

  # Critical: export. `verbose` includes default values (so the backup is a
  # full restore-grade snapshot, not just user customisations). `show-sensitive`
  # includes WireGuard keys / passwords so the backup is actually useful.
  # /export with file= is synchronous on v7+ — the file exists when the
  # command returns. A short delay is enough for the writer to flush.
  :put "[backup] step=export attempting"
  /export terse compact show-sensitive file=wantastic-export
  :delay 2s

  # Find the file. RouterOS appends .rsc automatically.
  :local fid [/file find where name="wantastic-export.rsc"]
  :if ([:len $fid] = 0) do={
    :put "[backup] FAIL: wantastic-export.rsc not found after 2s — /export may not be supported on this build"
    :return false
  }
  :local fsize [/file get $fid size]
  :if ($fsize = 0) do={
    :put "[backup] FAIL: export file is empty"
    :return false
  }
  :put ("[backup] step=find ok size=" . $fsize)

  # Build URL via concatenation — no parser ambiguity with ? = and base64.
  :local url ($baseURL . "?token=" . $wantasticToken . "&name=" . $bName)

  # Upload directly from disk via src-path= instead of reading the file into
  # a script variable. RouterOS's `contents` property is capped at ~65 KB;
  # large configs (verbose export of a busy router) silently return empty.
  # src-path= streams the file off flash with no such limit.
  :local uploaded false
  :do {
    /tool fetch http-method=post src-path=wantastic-export.rsc url=$url mode=https output=none check-certificate=no
    :set uploaded true
  } on-error={
    :put "[backup] FAIL: upload failed"
    :log warning "WANTASTIC-WG: backup upload failed"
  }
  :if ($uploaded = false) do={
    :do {/file remove [find where name~"wantastic-export"]} on-error={}
    :return false
  }
  :put "[backup] step=upload ok"
  :log info "WANTASTIC-WG: backup uploaded"

  :do {/file remove [find where name~"wantastic-export"]} on-error={}
  :put "[backup] step=done"
}

:global wantasticRestore do={
  :global wantasticCfg
  :global wantasticToken
  :global wantasticName
  :global wantasticDeploy

  :if ([:len $wantasticCfg] = 0) do={
    :put "[restore] FAIL: no saved config (deploy must run first)"
    :return false
  }
  :put "[restore] step=start"

  :local cfg $wantasticCfg
  :local vals [:toarray ""]
  :local i 0
  :while ([:typeof [:find $cfg "|" 0]] != "nil") do={
    :local pos [:find $cfg "|" 0]
    :set ($vals->$i) [:pick $cfg 0 $pos]
    :set cfg [:pick $cfg ($pos + 1) [:len $cfg]]
    :set i ($i + 1)
  }
  :set ($vals->$i) $cfg

  :if ($i < 4) do={
    :put "[restore] FAIL: saved config string is incomplete"
    :return false
  }

  $wantasticDeploy privateKey=($vals->0) publicKey=($vals->1) endpoint=($vals->2) address=($vals->3) allowedIPs=($vals->4) mtu=($vals->5) keepalive=($vals->6) backupToken=($wantasticToken) peerName=($wantasticName)
  :put "[restore] step=done"
}

:put "Wantastic RouterOS Library loaded (v1.1)"
