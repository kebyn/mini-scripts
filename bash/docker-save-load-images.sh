#!/bin/bash

# Save docker images in parallel (4 workers)
ds() {
  docker images --format '{{.Repository}}:{{.Tag}}' | xargs -t -n 1 -I {} -P 4 sh -c 'docker save {} > $(echo "{}" | sed "s/^.*\///").tar.gz'
}

# Load docker images in parallel (4 workers)
dl() {
  ls ./*.tar.gz | xargs -t -n 1 -I {} -P 4 sh -c 'docker load -i {}'
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  case "${1:-}" in
    save|ds) ds ;;
    load|dl) dl ;;
    *)
      echo "Usage: source $0 (then run ds or dl) OR $0 [save|load]" >&2
      exit 1
      ;;
  esac
fi
