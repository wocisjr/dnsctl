# dnsctl — návrhové poznámky

Pracovní poznámky, ne dokumentace. README je veřejná verze.

## Kontext

Doma mám dnsmasq konfigurovaný ručně. V práci jedeme DNS přes Ansible, takže tam
tenhle problém nemám — ale doma to znamená SSH, editace souboru, `systemctl reload`
a doufání. Tenhle nástroj to má nahradit.

Homelab: služby v oddělených VLANách (dnsmasq, Navidrome, Lidarr, Proxmox).

Záměrně malý projekt — cíl je **mít ho hotový a používat ho**, ne postavit
nejobecnější možný DNS nástroj. Pro deset hostů je to objektivně overkill; hodnota
je v tom pattern (validace → plan → atomický apply → rollback), který je stejný
jako u velkých systémů, jen bezpečně osahatelný.

## CLI

```
dnsctl validate    # jen kontrola, žádné side effecty  → běží v CI na PR
dnsctl plan        # diff proti živě nasazenému stavu
dnsctl apply       # aplikuj z lokálního souboru       → push model
dnsctl sync        # git pull + apply když se změnil commit → pull model, systemd timer
```

`sync` je hlavní režim. `apply` z něj padá zadarmo.

## Reconcile smyčka (`sync`)

```
1. git fetch do cache adresáře
2. HEAD stejný jako naposledy aplikovaný?   → no-op, konec
3. parse + validace hosts.yaml
4. vyrenderuj config
5. shodný s tím, co je na disku?            → zapiš commit, konec   (idempotence)
6. záloha současného configu
7. zapiš nový + `dnsmasq --test`
8. reload + health check
9. health check selhal? → obnov zálohu, reload, exit != 0
10. zapiš aplikovaný commit a stav
```

Kroky 2 a 5 jsou dvě různé zkratky. Krok 2 je levný (jen porovnání commitu),
krok 5 chytí případ, kdy se commit změnil, ale vyrenderovaný výstup ne.

## Validace

Tohle je věc, kterou dnsmasq neumí a kvůli které nástroj existuje:

- duplicitní IP napříč hosty
- duplicitní jméno nebo alias
- alias kolidující s jménem hosta
- IP mimo rozsah VLANy, do které je host přiřazen
- IP spadající do DHCP poolu
- odkaz na neexistující VLAN
- syntakticky nevalidní hostname

Chyby se vypisují všechny naráz, ne první a konec.

## Health check

**Ne `systemctl is-active`.** Skutečný DNS dotaz na `127.0.0.1` na kanárkové jméno
(nějaký host, který v `hosts.yaml` určitě je) a ověření, že odpověď sedí.

Rozdíl mezi „proces běží" a „DNS funguje" je celý smysl toho kroku. dnsmasq umí
naběhnout a přitom neodpovídat správně.

## Gotchas

**1. Chicken-and-egg.**
DNS stroj potřebuje resolvovat `github.com`, aby si stáhl config. Když si rozbiju
DNS, nemůže se sám opravit. Řešení: upstream resolver v dnsmasq napevno, nebo
remote zadaný přes IP. Patří to do README jako known limitation.

**2. Rollback musí přežít pád nástroje.**
Když `dnsctl` spadne mezi krokem 7 a 9, musí na disku zůstat dost na to, aby další
běh poznal rozdělaný stav a dokončil recovery. Záloha na pevném místě + marker
soubor, ne temp adresář.

**3. Deploy key.**
`sync` na DNS stroji potřebuje read-only deploy key na repo. Ne osobní SSH klíč.

## Metriky

Textfile pro node_exporter, ne HTTP server — pár řádků kódu, a jde na to alertovat.

```
dnsctl_last_sync_success_timestamp
dnsctl_sync_failed_total
dnsctl_rollback_total          ← tahle je nejzajímavější
dnsctl_records_total
```

Alert, který za to chci: „DNS stroj se 30 minut nezesynchronizoval."

## Knihovny

| Účel | Volba | Proč |
|---|---|---|
| git | `go-git/go-git` | čistě Go, na hostu nemusí být `git` |
| YAML | `goccy/go-yaml` | lepší chybové hlášky než `yaml.v3` |
| DNS dotaz | `miekg/dns` | health check |
| CLI | stdlib `flag` | čtyři příkazy, cobra je zbytečná |

## Plán (~3 dny)

**Den 1** — schéma `hosts.yaml`, parser, validátor, `dnsctl validate`.
Testy na table-driven fixtures. Žádný I/O na systém.

**Den 2** — renderer, `plan` s diffem, `apply` s atomickým zápisem, `dnsmasq --test`,
health check, rollback. Tady je většina rizika.

**Den 3** — `sync` s go-git, systemd timer unit, textfile metriky, CI (`validate` na PR),
README dopsat.

## Otevřené otázky

- Podporovat i DHCP reservace (`dhcp-host=`), nebo zůstat čistě u DNS? Zatím jen DNS.
- Víc souborů (`hosts.yaml` per VLAN), nebo jeden? Zatím jeden, dokud nebolí.
- Chci `plan` číst stav ze živého configu, nebo z posledního zapsaného stavu?
  Ze živého configu — jinak nezachytí ruční editaci na serveru.
