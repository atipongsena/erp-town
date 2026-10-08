#!/bin/bash
# Vendored from frappe_docker (https://github.com/frappe/frappe_docker), file resources/core/main-entrypoint.sh
# at upstream commit e08b50ca057139c6e8c9117838ee3f5c5ee64a37 (2026-10-06), unmodified below this header.
#
# MIT License
# Copyright (c) 2017 Frappe Technologies Pvt. Ltd.
# Permission is hereby granted, free of charge, to any person obtaining a copy of this software and
# associated documentation files (the "Software"), to deal in the Software without restriction,
# including without limitation the rights to use, copy, modify, merge, publish, distribute,
# sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions: the above copyright notice and this
# permission notice shall be included in all copies or substantial portions of the Software.
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND (full text: ./LICENSE).
set -e

umask 0002

ASSETS_PATH="/home/frappe/frappe-bench/sites/assets"
BAKED_PATH="/home/frappe/frappe-bench/assets"

echo "Linking fresh assets to volume..."
rm -rf "$ASSETS_PATH"
mkdir -p "$(dirname "$ASSETS_PATH")"
ln -s "$BAKED_PATH" "$ASSETS_PATH"

exec "$@"
