# Third-party rule data notices

This directory contains six unmodified public rule sets from
[MetaCubeX/meta-rules-dat](https://github.com/MetaCubeX/meta-rules-dat).
The exact data commit, per-file paths, sizes and SHA-256 digests are recorded
in `manifest.json`; the initial bundled snapshot is
`6fc21bcb856169abdf8589e47d6ee210d95ea09d` (2026-11-01).
Only local filenames have been changed to distinguish GeoIP and GeoSite data.
No subscription, account, proxy-node or router configuration is included.

The adjacent `source/*.json` files are the original editable JSON rule sets
from the same data commit, not data reconstructed from a user configuration.
They can be rebuilt with `sing-box rule-set compile source/<name>.json
--output <name>.srs` using sing-box 1.14.4. The maintenance script also checks
that this reproduces the bundled binary byte for byte. Runtime binaries embed
only SRS data and the manifest. Releases and container images provide the
source and notices under `licenses/ruleset-seed/`; `SOURCE.txt` in that package
links to this project's source directory at the exact build commit. The
automatic GitHub source archive for that commit also contains this directory.

## Licenses and attribution

The generated `sing` branch does not itself carry license files. MetaCubeX's
source repository declares GPL version 3; its full license is preserved in
`licenses/MetaCubeX-meta-rules-dat-GPL-3.0.txt`. Data is aggregated from other
projects whose notices and applicable terms remain with their contributions;
bundling the collection does not replace those licenses or claim ownership.
Full license texts are included here, with their exact retrieval sources:

| Project and contribution | License text | Reference commit |
| --- | --- | --- |
| MetaCubeX/meta-rules-dat: aggregation and conversion | [GPL-3.0](licenses/MetaCubeX-meta-rules-dat-GPL-3.0.txt) | [4178770badecb1b349fbcd62c737e0d7a2079729](https://github.com/MetaCubeX/meta-rules-dat/tree/4178770badecb1b349fbcd62c737e0d7a2079729) |
| v2fly/domain-list-community: GeoSite lists, including private and advertising domains; copyright (c) 2018-2019 V2Ray | [MIT](licenses/v2fly-domain-list-community-MIT.txt) | [01add61fbaf719b55673d969615cd328e80f214c](https://github.com/v2fly/domain-list-community/tree/01add61fbaf719b55673d969615cd328e80f214c) |
| Loyalsoldier/domain-list-custom: domain-list customization and build tooling; copyright (c) 2020 Loyalsoldier | [MIT](licenses/Loyalsoldier-domain-list-custom-MIT.txt) | [efacb51b8950ae673ebb6dcb9e7ecdd1decb1b6d](https://github.com/Loyalsoldier/domain-list-custom/tree/efacb51b8950ae673ebb6dcb9e7ecdd1decb1b6d) |
| blackmatrix7/ios_rule_script: ChinaMax domain input for GeoSite-CN | [GPL-2.0](licenses/blackmatrix7-ios_rule_script-GPL-2.0.txt) | [0b6897dc17e0c513d25449c935a9694818e32f0c](https://github.com/blackmatrix7/ios_rule_script/tree/0b6897dc17e0c513d25449c935a9694818e32f0c) |
| Loyalsoldier/geoip: CN and private IP data | [CC-BY-SA-4.0](licenses/Loyalsoldier-geoip-CC-BY-SA-4.0.txt), [GPL-3.0](licenses/Loyalsoldier-geoip-GPL-3.0.txt) as declared upstream | [084eb113e458b82374919dabefbc49cd2c2dd079](https://github.com/Loyalsoldier/geoip/tree/084eb113e458b82374919dabefbc49cd2c2dd079) |
| felixonmars/dnsmasq-china-list: Google/Apple domains included in the proxy-domain aggregate; copyright 2013 Felix Yan | [WTFPL-2.0](licenses/felixonmars-dnsmasq-china-list-WTFPL.txt) | [a8942d3ac6ebd8dcfc582d4e97fbb443a2176cb2](https://github.com/felixonmars/dnsmasq-china-list/tree/a8942d3ac6ebd8dcfc582d4e97fbb443a2176cb2) |
| GFWList: proxy-domain input to GeoLocation-!CN | [LGPL-2.1](licenses/gfwlist-LGPL-2.1.txt) | [efbdc355fb2aa9f6265fc9d2dd8d232a5444145c](https://github.com/gfwlist/gfwlist/tree/efbdc355fb2aa9f6265fc9d2dd8d232a5444145c) |
| cokebar/gfwlist2dnsmasq: GFWList conversion tool used by MetaCubeX | [GPL-3.0](licenses/cokebar-gfwlist2dnsmasq-GPL-3.0.txt) | [202e52fb5ea2260abc9f162b3a78e58ec9187005](https://github.com/cokebar/gfwlist2dnsmasq/tree/202e52fb5ea2260abc9f162b3a78e58ec9187005) |

This product includes GeoLite2 data created by MaxMind, available from
[https://www.maxmind.com](https://www.maxmind.com), through Loyalsoldier's
GeoIP collection. MetaCubeX converts the upstream data to sing-box's JSON/SRS
formats; WEBUI.for.SingBox does not alter the contents of these six snapshots.

`UPSTREAM-README.md` preserves MetaCubeX's description and acknowledgments.
`source/upstream-build.yml` records its public build recipe at the MetaCubeX
source-reference commit above. The license-reference commits record the
versions inspected for these notices, not a claim that every transitive input
was built from those exact revisions: the upstream recipe also reads moving
branches. The bundled JSON and SRS themselves are pinned and hashed.

Keep this notice, the full license texts and the matching source available
when redistributing these resources. The resources are supplied without
warranty, under their respective license terms.
