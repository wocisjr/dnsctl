# Git vrstva — co by měla umět

Pracovní poznámky k první fázi. Lidsky, ne jako API dokumentace.

## Kde jsme teď

`main.go` klonuje repo do paměti (`memory.NewStorage()` + `memfs`), pullne a vypíše
HEAD. Jako proof of concept fajn, ale pro `sync` to má jeden zásadní problém:
**paměťový klon nic nepamatuje.** Při každém spuštění stáhne celé repo znovu a
krok 2 reconcile smyčky („HEAD stejný jako naposledy?") nemá o co se opřít.

Navíc se tam klonuje i pullne — po čerstvém klonu není co pullovat.

---

## 1. Umět se poprvé usadit, a pak už jen dohánět

Nástroj běží na DNS stroji každých pár minut z systemd timeru. Napoprvé musí repo
stáhnout do trvalého adresáře na disku (třeba `/var/lib/dnsctl/repo`). Napodruhé
a dál už jen doběhne, co přibylo.

Tedy: **„je adresář prázdný? klonuj. Není? otevři ho a fetchni."** Nikdy obojí.

Rozdíl proti současnému kódu: `memory.NewStorage()` a `memfs.New()` nahradit
klonem na disk. Bez toho je každý sync plný download.

## 2. Fetch, ne pull

`Pull` znamená „stáhni a rovnou přepiš pracovní adresář". To nechceme — chceme
nejdřív **vědět, co přišlo**, rozhodnout se, a teprve pak sáhnout na soubory.

Fetch stáhne data, podíváme se na nový commit, checkout uděláme až jako vědomý
krok. Tohle je přesně ta pojistka, aby se rozbitý config nedostal na disk dřív,
než ho někdo zvaliduje.

## 3. Říct, na jakém commitu právě jsme

Krátce a jednoznačně: hash, čas, autor, zpráva. Potřeba na tři věci:

- porovnání s posledním aplikovaným stavem
- výpis v `plan` („jdeš z `abc123` na `def456`, tohle jsou commity mezi tím")
- logování, aby bylo za tři měsíce jasné, kdo co nasadil

## 4. Zamknout se na konkrétní větev

Ne „cokoliv, co je zrovna HEAD", ale explicitně `main` (nebo co se nastaví).
Jinak stačí, aby někdo pushnul feature branch, a nástroj nasadí něco, co nikdo
neschválil.

Do budoucna stojí za zvážení i pinnutí na tag — „nasazuj jen to, co je otagované".

## 5. Autentizace deploy keyem

Ne osobní SSH klíč z `~/.ssh` (viz gotcha 3 v DESIGN.md). Nástroj by měl umět vzít
cestu ke klíči z konfigu nebo proměnné prostředí, a fungovat i s veřejným repem
bez klíče.

Vedlejší věc, na kterou se zapomíná: **known_hosts**. Buď mít soubor s otiskem
GitHubu, nebo to explicitně vypnout — jinak to na čerstvě nainstalovaném stroji
spadne na tom, že hosta nezná.

## 6. Přečíst soubor z repa bez sáhnutí na pracovní adresář

Umět říct „dej mi obsah `hosts.yaml` na commitu `abc123`". Bez checkoutu.

To dá `plan` zadarmo — porovná se, co je nasazené, s tím, co by se nasadilo, a nic
se přitom nezmění na disku.

## 7. Ustát rozbitý stav lokálního klonu

Adresář někdo smaže. Nebo se do něj přihlásí a hrábne mu do útrob. Nebo fetch
spadne v půlce a zůstane po něm nedopsaný objekt.

Git vrstva by měla poznat „tohle už není použitelný klon" a spravit to tím
nejhloupějším možným způsobem: smazat a naklonovat znovu. Je to pár set kilobajtů,
není co řešit.

## 8. Nespadnout, když není síť

Fetch musí mít timeout — jinak systemd timer nechá viset proces do nekonečna a
další tick se s ním překrývá.

Když síť není, správná reakce **není** panika. Je to: „nemám nová data, jedu dál
s tím, co mám" a zvednutí čítače neúspěšných syncs. Nasazený config funguje dál,
jen se neaktualizuje. Přesně na to je alert „DNS stroj se 30 minut nezesynchronizoval".

Související: `panic(err)` na pěti místech v `main.go` je věc, kterou je potřeba
nahradit dřív než později. Na stroji, který běží z timeru, chceme návratový kód
a řádek do logu, ne stack trace.

## 9. Zapamatovat si, co bylo naposledy úspěšně aplikované

Přísně vzato ne git vrstva, ale visí to na ní. Někde na disku malý soubor: tenhle
commit prošel validací, nasadil se a health check ho potvrdil. Tohle je jediné,
co odpovídá na otázku z kroku 2 reconcile smyčky.

Pozor na detail — ukládá se to **až po úspěšném health checku**, ne po checkoutu.
Jinak si nástroj po pádu bude myslet, že rozbitý commit je nasazený a v pořádku.

---

## Hranice

Git vrstva je **jen doručovatel**. Její práce končí ve chvíli, kdy řekne „tady máš
obsah `hosts.yaml` a hash, ze kterého pochází".

Nic nevaliduje, nic nezapisuje do `/etc`, nic nereloaduje.

Čím tvrději se tahle hranice udrží, tím líp se to bude testovat — validátor pak
dostane string a nepotřebuje ke svým testům vůbec žádné repo.
