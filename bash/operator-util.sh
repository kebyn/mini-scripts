#!/bin/bash

set -o pipefail
set +o xtrace

red=$(tput setaf 1 2>/dev/null || printf '\033[31m')
green=$(tput setaf 2 2>/dev/null || printf '\033[32m')
reset=$(tput sgr0 2>/dev/null || printf '\033[0m')

function os_type() {
    OS=${OSTYPE//[0-9.-]*/}
    case "$OS" in
        darwin) machine=Mac ;;
        linux)  machine=Linux ;;
        *)
        echo "Operating System $OSTYPE not supported, supported types are darwin, linux"
        exit 1
        ;;
    esac
}

## Find OS type
os_type

function echo_fail() {
  printf "\e[31m✘ %s\033\e[0m\n" "${1}"
}

function echo_pass() {
  printf "\e[32m✔ %s\033\e[0m\n" "${1}"
}

function die() {
    echo "${red}$*${reset}"
    exit 1
}

# retry ${retries} "ls"
function retry() {
  local retries=$1
  shift
  local count=0
  until "$@"; do
    exit=$?
    wait=$((2 ** count))
    count=$((count + 1))
    if [[ ${count} -lt ${retries} ]]; then
      echo "Retry $count/$retries exited ${exit}, retrying in ${wait} seconds..."
      sleep ${wait}
    else
      echo "Retry $count/$retries exited ${exit}, no more retries left."
      exit 1
    fi
  done

  return 0
}

# run_cmd "ls -l" "${verbose}"
function run_cmd() {
  enable_debug=$2
  printf "Run Command:\n\t%s%s%s\n" "${green}" "$1" "${reset}"
  if [[ ${enable_debug} = "true" ]]; then
    eval "$1"
  else
    val=$(eval "$1" 2>&1)
  fi
  if [[ $? != 0 ]]; then
      die "${red}Unable to execute ${1} ${val} ${reset}"
  fi
}
