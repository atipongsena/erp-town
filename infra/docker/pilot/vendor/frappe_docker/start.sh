#!/bin/bash
# Vendored from frappe_docker (https://github.com/frappe/frappe_docker), file resources/core/start.sh
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

#Gunicorn defaults
GUNICORN_THREADS=${GUNICORN_THREADS:-4}
GUNICORN_WORKERS=${GUNICORN_WORKERS:-2}
GUNICORN_TIMEOUT=${GUNICORN_TIMEOUT:-120}

echo "Booting Gunicorn with $GUNICORN_WORKERS workers and $GUNICORN_THREADS threads..."

exec /home/frappe/frappe-bench/env/bin/gunicorn \
  --chdir=/home/frappe/frappe-bench/sites \
  --bind=0.0.0.0:8000 \
  --threads="$GUNICORN_THREADS" \
  --workers="$GUNICORN_WORKERS" \
  --worker-class=gthread \
  --worker-tmp-dir=/dev/shm \
  --timeout="$GUNICORN_TIMEOUT" \
  --preload \
  frappe.app:application
