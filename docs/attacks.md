# Attack taxonomy

GHOST NET models an intrusion as an **attack graph**. The attacker's planner
enumerates the actions available in the current state, scores each one (success
probability × target value − detection risk, weighted by profile), commits to
the best, and **replans** when a path is cut. Every action is an abstract event
with a duration, a success probability and a detection probability — never a
real network operation.

Actions are inspired by MITRE ATT&CK tactics:

| In-game action | ATT&CK tactic | Model effect |
|----------------|---------------|--------------|
| `recon` | Reconnaissance (T1595 Active Scanning) | learns a host exists; may raise a low-severity alert (radar sweep in 3D) |
| `exploit` | Initial Access / Execution (T1190 Exploit Public-Facing App) | compromises a host via a vulnerable service |
| `bruteforce` | Credential Access (T1110 Brute Force) | compromises a host via a weak credential; noisier |
| `lateral` | Lateral Movement (T1021 Remote Services) | pivots from an owned host to a reachable one |
| `persist` | Persistence (T1053 Scheduled Task) | survives a reboot; defeats `restart` (needs isolate/reimage) |
| `sensor` | Defense Evasion (T1562 Impair Defenses) | silences a host log agent or blinds a zone IDS |
| `exfil` | Exfiltration (T1041) | steals crown-jewel data in chunks until 100% = breach |

## Reachability (firewall policy)

The attacker can only move along allowed edges:

```
internet → DMZ
DMZ      → DMZ, SRV
SRV      → SRV, CORP
CORP     → everywhere
```

So a typical kill chain is `internet → DMZ foothold → SRV (crown jewel) → exfil`,
or it pivots through CORP workstations. The generator guarantees at least one
viable DMZ entry point and a crackable path to the crown jewel, so every seed is
winnable *for the attacker* if you do nothing — you must actually defend.

## Attacker profiles

| Profile | Behaviour |
|---------|-----------|
| `smash` | Fast and loud. Hammers services, accepts high detection. Breaches quickly if ignored. |
| `stealth` | Slow and quiet. Heavily discounts noisy actions, disables sensors early. |
| `apt` | Patient and methodical. Values persistence and depth, balances speed against risk. |

## Detection & fog of war

Detection is probabilistic and depends on the action, the attacker profile, and
whether the relevant sensor is alive. You only see alerts and logs for hosts
whose **log agent** is reporting or whose **zone IDS** is up. If the attacker
silences those sensors, the activity goes dark — the central tension of the
game. A "true positive" alert is what the incident report uses to measure your
detection latency (dwell time).

## Player counters

- `patch <host> <service>` removes the flaw an action relies on → that path
  fails validation and the planner replans.
- `isolate <host>` cuts a foothold off entirely (counts as downtime).
- `restart <host>` clears a **non-persistent** foothold; `forensics` reveals
  whether persistence is present first.
- `block <ip>` drops the attacker's current source (read it from the alert).
- Over-reacting is punished: blocking a legitimate asset IP or reconnecting a
  still-compromised host costs score.
