# ERP Town art bible — v1

Status: superseded duplicate draft. The user confirmed that art is already being
developed in `D:/ERP_Town-art-direction/`. This draft and its generated mood board
are retained as history; they are not approved references for implementation.
The existing Sunny Office Town direction, palette and floating AI characters take
precedence over the proposals below. See the
[art handoff record](../superpowers/plans/2026-10-10-art-foundation.md) for sources.
No additional review of this duplicate draft is required.

## Visual intent

A calm, crafted office town that makes the sales → delivery → invoice → payment
flow readable in a few seconds. Architectural miniature proportions, matte surfaces,
clear silhouettes and warm daylight communicate a working place. Use a compact
island, connected streets and one common asset grammar. Show work status rather than
employee performance. Every eventual live signal must link to authorized ERP data.

The mood board is an AI-generated **concept reference**, not a running-client
screenshot or evidence that models, rigs, textures or performance are implemented.
Its prompt is preserved in [moodboard-v1.prompt.md](moodboard-v1.prompt.md).
The source image will be stored beside this document as `moodboard-v1.png`.

## Palette and material rules

| Token | sRGB | Use |
| --- | --- | --- |
| Ivory | `#F6EEDC` | façades, warm neutral ground, panels |
| Ink | `#283C46` | roofs, outlines, readable UI text |
| Mint | `#87B7A8` | warehouse identity, vegetation accents |
| Coral | `#D88370` | sales identity, selected warm accents |
| Brass | `#D8AF69` | accounting identity, small warm lights |
| Indigo | `#6D7C99` | IT identity, cool roof accents |
| Stone | `#C9CED0` | paving, curb and structural trim |

Department colors are identifiers, not success/error indicators. Pair operational
status with an icon and text: clock/pending, check/complete, exclamation/error.
Avoid relying on red/green distinctions. Validate real UI text contrast in D;
the concept board does not certify accessibility. Use Sarabun for Thai UI, with
legible HTML labels rather than text baked into tiny 3D objects.

Retint the selected KayKit family into one shared palette/gradient atlas. Match the
custom department assets to its observed bevels, silhouette density and proportions.
Matte painted surfaces dominate; glass and metal are restrained accents. Do not
mix unrelated kits or replace the agreed family with generated production models.

## Department grammar and document flow

| Building | Recognizable silhouette | Data-driven prop |
| --- | --- | --- |
| Sales | Low storefront, broad awning, document window | Bounded order tray |
| Warehouse | Broad stepped roof, loading door and dock | Crates and delivery cart |
| Accounting | Compact bank, vault arch and side ledger window | Invoice tray and overdue clock |
| IT | Small workshop, angled roof and antenna | Activity light and tool board |
| HR | Civic hall, welcoming entrance | Permission-filtered office board |
| Administration | Modest central civic tower | Agent pause/status sign |

Place Sales, Warehouse and Accounting along the same visible road. Give each a
distinct silhouette at overview zoom; color is secondary. Keep IT off the business
flow so dev activity cannot look like an ERP transaction. HR and administration
are visual landmarks until their real data flows exist. Do not invent live counts.

Use a one-meter modeling unit and modular half-meter footprints. Keep roof heights
and door scale consistent. Document trays show a capped stack plus an exact UI
count when authorized, never an unbounded object per record. Cancellation updates
the matching signal; payment activity alone must not label an invoice paid.
Overdue and stock signals follow current reconciled metadata from C.

## Camera, light and composition

Orthographic isometric camera: start with 45° azimuth and 35.264° elevation above
the ground plane. Use a fixed hero orientation for screenshot comparisons. Allow
user orbit/zoom later while keeping labels upright and landmarks unobscured.
Reserve quiet space around the town for HTML controls and a document panel.

Use Blender Cycles for baked AO/lightmaps and an AgX reference view. D should match
the reference with its agreed tone mapping, N8AO, SMAA and one LUT. Avoid baking
the same strong occlusion twice. Twilight has warm readable window lights; bloom
is limited to small emissive lights. Time of day is presentation, not an inference
about company financial health. No dependency or rendering setting is installed
or claimed verified at this gate.

## Characters and motion

One current KayKit humanoid rig family for both people and agents; verify the
downloaded rig and animation files before export. A human has ordinary office
clothes and a role accessory. An AI agent adds a simple visor and antenna with a
clearly readable silhouette. Share body proportions, atlas and animation skeleton.
No legacy legless rig mixed with current humanoid animation sets.

Human avatars remain opt-in; absence must not look like inactivity. XP/level belongs
only to AI agents. Preserve role accessories while keeping skin tones natural.
Do not show names, salaries or private tasks in shared street labels.

Animate only truthful transitions: short anticipation before a delivery departure,
gentle easing for document arrival, one restrained error gesture. Do not endlessly
loop successful business activity. Reduced motion removes traversal/pulsing and
uses stable icons and text; operational status remains available in Classic view.

## Source assets and provenance

Verified 2026-10-10 against the author's own pages:

- [KayKit City Builder Bits](https://kaylousberg.itch.io/city-builder-bits): the free
  tier provides low-poly city assets and glTF, under CC0. Source `.blend` and extra
  tiers are paid; no purchase has been authorized or made.
- [Current KayKit Character Animations](https://kaylousberg.itch.io/kaykit-character-animations):
  the free pack supports `Rig_Medium` and `Rig_Large`, with glTF under CC0. Select
  one compatible character family before importing. The older legacy animation
  page is a different rig generation.

Keep original archives/license files, source URLs, hashes and export notes in an
asset manifest at import time. Custom department models are authored in Blender;
concept image generation does not transfer an asset pack's provenance to them.
No downloaded models or licenses have been imported by this step.

## Review sequence and acceptance

1. Review this mood board and the proposed art direction: silhouette, palette,
   warmth, human/AI distinction and compact document flow.
2. Produce daylight and twilight style frames from actual selected/imported assets
   plus a custom department prototype. Compare them to this direction.
3. Produce a Blender greybox and screenshots at overview and working zoom; review
   flow, occlusion, building selection and panel space.
4. Implement D only after the required A screenshots are reviewed. Validate the
   real client on the target device and with reduced motion.

These reviews come from the approved program overview's art gates, not an extra
approval workflow introduced by a skill. Dependencies, purchases, real credentials,
deployments and external writes retain the repository's separate approval boundaries.
