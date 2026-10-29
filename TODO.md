# dnsctl v Pythonu — TODO a návod

Přepis plánu z DESIGN.md a FUNKCE.md do Pythonu. Design (validate → plan → apply →
sync, rollback, health check) zůstává. Mění se jazyk, knihovny a pár detailů,
které v Pythonu vyjdou jinak.

---

## 0. Než začneš: co se přechodem mění

- **Ztrácíš single binary.** Go dávalo jeden soubor bez závislostí a bez `git` na
  hostu. V Pythonu potřebuješ na DNS stroji interpret + závislosti. Řešení v bodě 9.
- **Ztrácíš „na hostu nemusí být git".** Doporučuju to vzdát a volat systémový
  `git` přes `subprocess` (bod 5). Je to jednodušší a robustnější než
  pure-python git. Když na tom trváš, existuje `dulwich`.
- **Získáváš `ipaddress` ze stdlib** — kontrola „IP je ve VLANě / v DHCP poolu"
  je jeden řádek (`ip in network`).
- Kód je zatím ~30 řádků Go, takže přechod nic nestojí.

---

## 1. Úklid repa

- [ ] `git rm main.go go.mod go.sum`
- [ ] `git rm --cached dnsctl` — 14 MB binárka v gitu. Ta tam zůstane v historii
      i po smazání, ale aspoň ať nepřibývají další.
- [ ] `.gitignore`: `__pycache__/`, `.venv/`, `*.pyz`, `dist/`, `.pytest_cache/`
- [ ] Aktualizovat tabulku knihoven v DESIGN.md a zmínku o go-git v README.

## 2. Kostra projektu

Python **3.11+** (Debian 12 má 3.11 → dostaneš i `tomllib`, lepší tracebacky).

Nástroj: **uv** (`uv init --package dnsctl`, `uv add …`, `uv run pytest`).

```
dnsctl/
  pyproject.toml
  src/dnsctl/
    __main__.py     # python -m dnsctl
    cli.py          # argparse, 4 subcommandy, exit kódy
    model.py        # dataclassy: Vlan, Host, Config
    loader.py       # YAML → model, s čísly řádků
    validate.py     # Config → list[Chyba], žádné I/O
    render.py       # Config → str, deterministicky
    apply.py        # záloha, atomický zápis, test, reload, rollback, marker
    health.py       # DNS dotaz na kanárka
    gitsrc.py       # fetch, rev-parse, show — jen doručovatel
    state.py        # poslední aplikovaný commit, čítače
    metrics.py      # textfile pro node_exporter
  tests/
    fixtures/       # hosts.yaml varianty, dobré i rozbité
```

Hranice z FUNKCE.md platí i tady: `validate.py` a `render.py` dostávají data,
ne cesty k souborům. Testy pak nepotřebují disk ani repo.

## 3. Knihovny

| Účel | Volba | Proč |
|---|---|---|
| CLI | `argparse` (stdlib) | 4 příkazy, `click`/`typer` jsou zbytečné |
| YAML | `PyYAML` (`yaml.SafeLoader`) | je v apt (`python3-yaml`), čísla řádků jdou dostat (viz níž) |
| model | `dataclasses` (stdlib) | validace je stejně hlavně napříč záznamy, pydantic by pomohl jen u typů |
| IP/sítě | `ipaddress` (stdlib) | `ip_address(x) in ip_network(vlan)` |
| DNS dotaz | `dnspython` | je v apt (`python3-dnspython`), health check |
| git | `subprocess` + systémový `git` | viz bod 5 |
| diff pro `plan` | `difflib.unified_diff` (stdlib) | |
| zamykání | `fcntl.flock` (stdlib) | `apply` ručně vs. `sync` z timeru |
| testy | `pytest` (+ `parametrize` místo table-driven) | jen dev |
| lint/typy | `ruff`, volitelně `mypy` | jen dev |

Za zmínku: **runtime závislosti jsou jen dvě a obě jsou v Debian repu.** Na DNS
stroji tak jde `apt install python3-yaml python3-dnspython` a žádný venv.

Co nebrat: `GitPython` (jen obal nad binárkou `git`, přidá závislost a nic moc
nedá), `pygit2` (potřebuje libgit2), `prometheus_client` (čítače žijí jen v
procesu, mezi běhy je stejně musíš persistovat sám).

## 4. Den 1 — model, loader, validátor, `dnsctl validate`

- [ ] `model.py`: `Vlan(name, network: IPv4Network, dhcp_range | None)`,
      `Host(name, ip, vlan, aliases, line)`, `Config(domain, vlans, hosts, canary)`.
- [ ] `loader.py`: vlastní `SafeLoader`, který
  - u každého mappingu uloží číslo řádku (`node.start_mark.line + 1`) — chceš
    hlášky typu `hosts.yaml:14: duplicitní IP 10.10.30.11 (taky na řádku 9)`;
  - **hlásí duplicitní klíče.** PyYAML je jinak potichu přepíše posledním —
    dva `ip:` u jednoho hosta projdou bez varování.
- [ ] Striktní typy: `name` musí být `str`. YAML 1.1 udělá z `name: no` hodnotu
      `False` a z `name: 0123` číslo.
- [ ] `validate.py` — vrací **seznam** chyb, ne výjimku po první:
  - duplicitní IP
  - duplicitní jméno / alias, alias kolidující se jménem
  - IP mimo síť VLANy
  - IP v DHCP poolu
  - neexistující VLAN
  - nevalidní hostname (label `[a-z0-9-]{1,63}`, ne `-` na kraji)
  - kanárek musí existovat mezi hosty
- [ ] `cli.py validate` → exit 0 / 1, chyby na stderr.
- [ ] Testy: `@pytest.mark.parametrize` přes fixtures, každé pravidlo zvlášť +
      jeden soubor se vším rozbitým naráz.

## 5. Git vrstva (`gitsrc.py`)

Pokrývá body 1–8 z FUNKCE.md. Přes `subprocess.run([...], timeout=…, check=True,
capture_output=True)`.

**Doporučení: bare klon.** Nikdy nepotřebuješ pracovní adresář — soubor čteš
přímo z commitu. Odpadne celý problém „pull přepíše soubory dřív, než je
zvaliduješ" a není co rozbít checkoutem.

| Potřeba (FUNKCE.md) | Příkaz |
|---|---|
| 1. první klon / dohánění | neexistuje → `git clone --bare URL /var/lib/dnsctl/repo.git`, jinak `git fetch origin +refs/heads/main:refs/heads/main` |
| 2. fetch, ne pull | viz výš, bare klon nic jiného ani neumí |
| 3. info o commitu | `git log -1 --format=%H%x00%ct%x00%an%x00%s <rev>` |
| 3. commity mezi stavy (`plan`) | `git log --oneline OLD..NEW` |
| 4. zamknutá větev | refspec jen pro `main` (konfigurovatelné) |
| 5. deploy key | env `GIT_SSH_COMMAND="ssh -i KEY -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/etc/dnsctl/known_hosts"` |
| 6. soubor bez checkoutu | `git show <commit>:hosts.yaml` |
| 7. rozbitý klon | `git rev-parse --verify main` nebo `git fsck` selže → `shutil.rmtree` + klon znovu |
| 8. timeout | `timeout=60` v `subprocess.run` → `TimeoutExpired` → „žádná nová data", ne pád |

- [ ] Env pro všechny volání: `GIT_TERMINAL_PROMPT=0` (jinak git u HTTPS čeká na
      heslo a visí).
- [ ] Rozhraní ven jen něco jako `fetch() -> str` (hash) a
      `read_file(commit, path) -> str`. Nic dalšího.
- [ ] `known_hosts`: otisky GitHubu (`ssh-keyscan github.com` jednou, ověřit proti
      https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/githubs-ssh-key-fingerprints)
      a soubor dát do repa / instalace.

## 6. Den 2 — render, plan, apply, health check, rollback

> **⚠ Ověřit před implementací — týká se i Go verze:**
> `systemctl reload dnsmasq` posílá SIGHUP a dnsmasq po SIGHUP **znovu nečte
> konfiguraci** (`/etc/dnsmasq.d/*`), jen hosts soubory (`/etc/hosts`,
> `--addn-hosts`, `--hostsdir`). Změny v `address=` by se tedy reloadem
> neprojevily — health check by zrovna tohle chytil, ale jako rollback u každého
> applye.
>
> Dvě cesty:
> - **(doporučuju)** renderovat do **hosts formátu**
>   (`10.10.30.11 navidrome.home.example music.home.example`) do adresáře
>   v `--hostsdir`. Funguje SIGHUP, dnsmasq ho navíc sleduje přes inotify,
>   dostaneš i PTR záznamy a přesnou shodu jména.
> - nechat `address=` a dělat `systemctl restart` (krátký výpadek DNS).
>
> Navíc `address=/navidrome.home.example/…` odpovídá i na
> `cokoliv.navidrome.home.example` — wildcard, který asi nechceš.

- [ ] `render.py`: deterministický výstup (seřadit hosty, pevná hlavička
      `# GENERATED — do not edit`, `\n` na konci). Bez toho nefunguje krok 5
      reconcile smyčky (idempotence).
- [ ] `plan`: přečíst **živý** soubor z disku, `difflib.unified_diff` proti
      vyrenderovanému, k tomu `git log OLD..NEW`. Exit 0 = beze změn, 2 = změny
      (jako `terraform plan -detailed-exitcode`) — hodí se do skriptů.
- [ ] Atomický zápis: `tempfile.NamedTemporaryFile(dir=cílový_adresář, delete=False)`
      → write → `flush()` + `os.fsync()` → `os.replace()` → fsync adresáře.
      Temp musí být ve stejném adresáři (stejný filesystem), jinak `replace` není
      atomický.
- [ ] Záloha na **pevném** místě (`/var/lib/dnsctl/backup/…`) + marker soubor
      `apply-in-progress` (obsahuje commit). Zapsat **před** zápisem nového configu,
      smazat až po úspěšném health checku.
- [ ] Na začátku každého běhu: marker existuje? → předchozí běh spadl, obnov zálohu,
      reload, smaž marker, `rollback_total += 1`. (Gotcha 2 z DESIGN.md.)
- [ ] `dnsmasq --test` přes `subprocess`. Cesty k binárkám a reload příkaz
      v konfigu → v testech je nahradíš `true` / `false`.
- [ ] `health.py` s dnspython:
      ```python
      r = dns.resolver.Resolver(configure=False)
      r.nameservers = ["127.0.0.1"]
      r.lifetime = 2.0
      ans = r.resolve(f"{canary}.{domain}", "A")
      ```
      Ověřit, že IP sedí. Pár pokusů s krátkou pauzou — dnsmasq po restartu
      nenaběhne okamžitě.
- [ ] `fcntl.flock` na `/run/dnsctl.lock` kolem celého `apply`/`sync`.
- [ ] Testy: `tmp_path` jako fake `/etc` a `/var/lib`, fake příkazy. Scénáře:
      úspěch, `--test` selže, health check selže, pád uprostřed (marker zůstane).

## 7. Den 3 — sync, stav, metriky, systemd, CI

- [ ] `state.py`: JSON v `/var/lib/dnsctl/state.json` — poslední úspěšně aplikovaný
      commit, čas, čítače. Zápis stejně atomicky jako config.
      Commit se ukládá **až po health checku** (FUNKCE.md bod 9).
- [ ] `sync` = reconcile smyčka z DESIGN.md, složená z hotových kousků.
- [ ] Exit kódy, ať jde rozlišit v logu / alertu, např.:
      `0` ok, `1` nevalidní config, `2` apply selhal a rollback proběhl,
      `3` git/síť nedostupná, `4` rollback selhal (← tohle je poplach).
- [ ] Žádné tracebacky ven: v `main()` chytat známé výjimky, `logging` na stderr
      (journald si to vezme). Neočekávané nechat spadnout — to je bug.
- [ ] `metrics.py`: textfile do
      `/var/lib/prometheus/node-exporter/dnsctl.prom`, zápis atomicky
      (node_exporter jinak může přečíst půlku). Formát je jednoduchý, ručně:
      ```
      # TYPE dnsctl_rollback_total counter
      dnsctl_rollback_total 3
      ```
- [ ] systemd: `dnsctl-sync.service` (`Type=oneshot`, `TimeoutStartSec=120`,
      `User=` pokud jde, jinak root kvůli reloadu) + `dnsctl-sync.timer`
      (`OnUnitActiveSec=5min`, `RandomizedDelaySec=30`).
      Oneshot služba se nespustí znovu, dokud předchozí běh neskončí — overlap
      řeší systemd, flock je pojistka pro ruční `apply`.
- [ ] CI (GitHub Actions): `uv run ruff check`, `uv run pytest`,
      `uv run dnsctl validate hosts.yaml`.

## 8. Konfigurace nástroje

Malý `/etc/dnsctl/config.toml` (čte se `tomllib`, stdlib):
repo URL, větev, cesta k deploy key, cesta k výstupnímu souboru, příkaz pro
test / reload, cesta k metrikám. Rozumné defaulty, ať jde lokálně spustit
`dnsctl validate hosts.yaml` bez configu.

## 9. Nasazení na DNS stroj

Seřazeno od nejjednoduššího:

1. **apt + zipapp:** `apt install python3 python3-yaml python3-dnspython git`,
   projekt zabalit stdlib `python -m zipapp src -m dnsctl.cli:main
   -p "/usr/bin/env python3" -o dnsctl.pyz`. Jeden soubor, nejblíž Go binárce.
2. **shiv** — totéž, ale přibalí i závislosti (`shiv -c dnsctl -o dnsctl.pyz .`).
   Pak z apt stačí `python3` a `git`.
3. **`uv tool install git+https://github.com/wocisjr/dnsctl`** — vlastní venv,
   pohodlné, ale na DNS stroji potřebuješ uv.

Gotcha 1 z DESIGN.md (chicken-and-egg) platí beze změny.

---

## Pořadí, ve kterém bych to dělal

1. úklid + kostra + `validate` s testy (nic nesahá na systém)
2. ověřit chování dnsmasq reloadu (bod 6) **ručně na stroji** a rozhodnout formát
3. render + `plan`
4. `apply` s rollbackem a testy na pády
5. git vrstva
6. `sync`, stav, metriky, systemd, CI
