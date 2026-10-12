# Town client hardware observation

## Current observation after material integration

The current [disconnected report](client-renderer-evidence-2026-10-11.json) and
[character report](client-renderer-avatars-2026-10-11.json) were recorded on
2026-10-11 at 06:16 UTC on the same RTX 3080 Ti/ANGLE D3D11 configuration below.
No other browser test ran during these captures. Python tests ran independently
in Docker. Viewport remains 1440x900, canvas 1090x820, standard shadows and DPR 1.

| Measured load | Active frames per transition | Maximum p95 interval | Peak calls | Peak triangles |
| --- | --- | --- | --- | --- |
| Disconnected town | 65–67 | 16.8 ms | 275 | 413,228 |
| Two humans and one AI, local fixtures | 65–67 | 16.8 ms | 294 | 422,208 |

Both captures stay below 300 draw calls and 750,000 triangles. Glass exported
with transmission 1 caused an additional opaque-scene transmission pass. The
web adapter sets transmission/thickness to zero only on `M_glass` and
`M_vehicle_glass`, preserving their colors/PBR properties and the source Blender
file. Independent clone disposal preserves the shared GLTF cache. This removes
the redundant pass; it does not bake shadows. Existing water integration remains.
Humans do not cast dynamic shadows, and only two humans plus one AI indicator are
rendered. Their independent skeleton/material resources are released on unmount.

Reproduce the second capture with
`node packages/town-client/scripts/render-evidence.mjs http://127.0.0.1:5173 --avatars`.
All identity, world and avatar requests are intercepted locally, using the shared
generated protobuf descriptor. It loads all three actual skinned assets before
capturing. No ERP session, provider call or private data is involved.
[Captured overview](town-client-avatars-2026-10-11.png).

Frame intervals are requestAnimationFrame pacing, not GPU timer durations.
Mobile hardware, real connected-world load, texture/GPU-memory measurements,
postprocessing and named human art/comprehension acceptance remain open.
The earlier 413-call observation below is preserved as a baseline, with its
[original raw report](client-renderer-baseline-2026-10-11.json).

## Earlier baseline

Local date: 2026-10-11 (Asia/Bangkok). The figures below precede material optimization.
Reproduce with `node packages/town-client/scripts/render-evidence.mjs` while the
disconnected local preview is running. The script refuses remote targets and
software renderers; it never connects to ERP or invokes a model.

Hardware: AMD Ryzen 9 5900X, NVIDIA GeForce RTX 3080 Ti, driver 32.0.16.1714.
Chromium 153.0.8010.12 uses ANGLE Direct3D11 on NVIDIA with the script's explicit
D3D11/GPU flags. The default headless launch instead reported SwiftShader.
Viewport 1440x900; actual canvas 1090x820, device scale factor 1. Standard quality
and shadows enabled, reduced motion off. No avatar/agent/ERP load or postprocessing.

| Active transition | Rendered frames | p95 interval (ms) | Peak draw calls | Peak triangles |
| --- | --- | --- | --- | --- |
| Open Shop | 65 | 16.700 | 413 | 619,906 |
| Return overview | 66 | 16.700 | 411 | 619,650 |
| Open Warehouse | 68 | 16.734 | 413 | 619,906 |
| Return overview | 66 | 16.700 | 411 | 619,650 |
| Open Bank | 65 | 16.700 | 413 | 619,906 |
| Return overview | 64 | 16.700 | 411 | 619,650 |

The probe groups actual WebGL draws by requestAnimationFrame timestamps, including
shadow passes and excluding deliberate idle gaps between transitions. These are
active frame-pacing intervals, not GPU timer-query durations. Roughly 60 frames/s
here does not prove sustained performance under future connected load. This is
not a mobile/pilot measurement or an all-scene worst-camera search.

## Findings and open acceptance

Triangles stay below A's initial 750k budget. **Draw calls exceed the 300 budget**,
by up to 113; `withinRenderBudget` is false. All loaded meshes currently cast
real-time shadows, while the spec expects static baked lighting. Batch/bake
static shadow work and re-measure before adding actor density or certifying D's
final gate. Do not raise the budget or reinterpret earlier 241-call samples as
complete shadow-inclusive evidence. Texture count/GPU memory were not measured.

The initial probe exposed an idle-motion defect: returning to overview after
idle rendered only two frames. The first demand-rendered delta included the idle
duration, consuming the 0.5s roof transition and camera easing in one step. Both
now cap delta at 50ms. Normal short frame timing and direct reduced-motion changes
remain. The same probe then captured 64-68 frames per transition. The new browser
regression passed with SwiftShader in 21.0s, counting actual WebGL frames after
idle rather than UI animation callbacks. No assets/lighting/Blender source changed.

Remaining: static shadow/draw-call optimization, material/postprocessing
integration, texture/memory evidence, mobile/low-tier and connected-load hardware,
human motion/comprehension/design review and A's G-A3/G-A4 gates.
Source Blender SHA256 remains
`E9BEE83331BB4CF1649790BF884CF09F764911334E6CC842DB8B2625E226C933`.
