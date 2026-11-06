#!/bin/bash
# Pure Bash HTTP GET request using /dev/tcp

function __curl() {
  read -r proto server path <<<"$(echo "${1//// }")"
  DOC=/${path// //}
  HOST=${server//:*}
  PORT=${server//*:}
  [[ x"${HOST}" == x"${PORT}" ]] && PORT=80

  exec 3<>"/dev/tcp/${HOST}/${PORT}"
  echo -en "GET ${DOC} HTTP/1.0\r\nHost: ${HOST}\r\n\r\n" >&3
  (while read -r line; do
   [[ "$line" == $'\r' ]] && break
  done && cat) <&3
  exec 3>&-
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  if [[ $# -lt 1 ]]; then
    echo "Usage: $0 <http://host[:port]/path>" >&2
    exit 1
  fi
  __curl "$1"
fi
