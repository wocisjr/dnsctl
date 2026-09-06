# dnsctl

Declarative DNS management for dnsmasq, with validation and safe rollback.

> **Status:** early development. Design settled, implementation not started.

## The problem

dnsmasq is configured by hand-editing files under `/etc/dnsmasq.d/`. That is fine
until it isn't:

- A syntax error stops dnsmasq from starting — and since it sits on the critical
  path, the whole network loses DNS at once.
- Duplicate IPs and duplicate names are accepted silently.
- There is no overview of what is actually deployed.
- Three months later, nobody knows why a record exists.

## The idea

A single versioned YAML file is the source of truth. `dnsctl` renders it into
dnsmasq config, validates it, and applies it atomically — restoring the previous
config if the result is not a working resolver.

```yaml
# hosts.yaml
domain: home.example

vlans:
  services: 10.10.20.0/24
  media:    10.10.30.0/24

hosts:
  - name: navidrome
    ip: 10.10.30.11
    vlan: media
    aliases: [music]

  - name: lidarr
    ip: 10.10.30.12
    vlan: media

  - name: proxmox
    ip: 10.10.20.2
    vlan: services
```

renders to:

```
# /etc/dnsmasq.d/10-hosts.conf   (GENERATED — do not edit)
address=/navidrome.home.example/10.10.30.11
address=/music.home.example/10.10.30.11
address=/lidarr.home.example/10.10.30.12
address=/proxmox.home.example/10.10.20.2
```

## Commands

| Command | Effect |
|---|---|
| `dnsctl validate` | Check the source file. No side effects — this is what runs in CI. |
| `dnsctl plan` | Show the diff between the source file and what is deployed. |
| `dnsctl apply` | Render and apply from a local file. |
| `dnsctl sync` | Pull from git and apply if the commit changed. Runs on a timer. |

## Two things it does properly

**Rollback that actually holds.** Config is backed up before every write. If the
new config fails `dnsmasq --test`, or if dnsmasq comes back without resolving,
the previous config is restored and reloaded. A crash mid-apply leaves enough on
disk for the next run to recognise and finish the recovery.

**A health check that means something.** Not `systemctl is-active` — an actual DNS
query against `127.0.0.1` for a known canary name, with the answer verified. The
difference between *the process is running* and *DNS works* is the whole point.

## GitOps without the infrastructure

`dnsctl sync` runs on the DNS host itself, on a systemd timer. It polls git and
reconciles. No webhook, no runner, no inbound access to the host. With `go-git`,
the host does not even need `git` installed.

## Metrics

Written as a node_exporter textfile rather than by serving HTTP:

```
dnsctl_last_sync_success_timestamp
dnsctl_sync_failed_total
dnsctl_rollback_total
dnsctl_records_total
```

## License

TBD
