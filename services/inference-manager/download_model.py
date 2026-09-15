#!/usr/bin/env python3
"""Download one Hugging Face snapshot into a manager-owned temporary path."""

import argparse
from huggingface_hub import snapshot_download


parser = argparse.ArgumentParser()
parser.add_argument("--source", required=True)
parser.add_argument("--destination", required=True)
args = parser.parse_args()

repo_id, separator, revision = args.source.rpartition("@")
if not separator:
    repo_id, revision = args.source, None

snapshot_download(
    repo_id=repo_id,
    revision=revision,
    local_dir=args.destination,
    local_dir_use_symlinks=False,
)
