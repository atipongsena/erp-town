"""Export A's static town in a separate Blender process; never save the source."""
import hashlib
import json
import math
import sys
from pathlib import Path

import bpy
from mathutils import Matrix

args = sys.argv[sys.argv.index('--') + 1:]
source, target = Path(args[0]), Path(args[1])
before = hashlib.sha256(source.read_bytes()).hexdigest()
bpy.ops.wm.open_mainfile(filepath=str(source))
bpy.context.scene.frame_set(1)
buildings = {'Shop', 'Warehouse', 'Bank', 'HQ', 'IT', 'Post', 'ClockTower', 'CityGate'}
graph = bpy.context.evaluated_depsgraph_get()
parts = []
for original in list(bpy.context.scene.objects):
    if any(collection.name == 'ERP_Flow_Preview' for collection in original.users_collection):
        continue
    if original.type not in {'MESH', 'CURVE', 'FONT'}:
        continue
    current = original
    excluded = False
    while current:
        if (current.name in buildings or current.hide_render or
                current.get('actor_kind') in {'human', 'agent'} or
                current.name.startswith(('SLA_', 'Citizen_', 'Agent_', 'Traffic', 'DockTruck'))):
            excluded = True
            break
        current = current.parent
    if excluded:
        continue
    mesh = bpy.data.meshes.new_from_object(original.evaluated_get(graph), preserve_all_data_layers=True, depsgraph=graph)
    mesh.transform(original.matrix_world)
    copy = bpy.data.objects.new('ENV_' + original.name, mesh)
    bpy.context.scene.collection.objects.link(copy)
    parts.append(copy)
if not parts:
    raise RuntimeError('No environment geometry')
bpy.ops.object.select_all(action='DESELECT')
for part in parts:
    part.select_set(True)
bpy.context.view_layer.objects.active = parts[0]
bpy.ops.object.join()
merged = parts[0]
merged.name = 'Town_environment'
triangulate = merged.modifiers.new('web_triangles', 'TRIANGULATE')
bpy.ops.object.modifier_apply(modifier=triangulate.name)
target.parent.mkdir(parents=True, exist_ok=True)
bpy.ops.export_scene.gltf(filepath=str(target), export_format='GLB', use_selection=True,
                          export_animations=False, export_cameras=False, export_lights=False,
                          export_draco_mesh_compression_enable=True,
                          export_draco_mesh_compression_level=5,
                          export_draco_position_quantization=16)
conversion = Matrix.Rotation(-math.pi / 2, 4, 'X')
layout = []
for name in sorted(buildings):
    root = bpy.data.objects[name]
    position, rotation, scale = (conversion @ root.matrix_world @ conversion.inverted()).decompose()
    layout.append({'name': name, 'position': list(position),
                   'quaternion': [rotation.x, rotation.y, rotation.z, rotation.w], 'scale': list(scale)})
(target.parent / 'layout.json').write_text(json.dumps({'source_sha256': before, 'buildings': layout}, indent=2) + '\n', encoding='utf-8', newline='\n')
if hashlib.sha256(source.read_bytes()).hexdigest() != before:
    raise RuntimeError('Source changed during export')
print(json.dumps({'source_sha256': before, 'triangles': len(merged.data.polygons),
                  'material_slots': len(merged.data.materials), 'bytes': target.stat().st_size,
                  'source_unchanged': True}), flush=True)
