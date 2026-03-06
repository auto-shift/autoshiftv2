# !/bin/bash
# Terminal settings for AutoShift scripts
# This script is not called directly, but sourced by other scripts to set up color variables 

set -e

# Check if the output is a terminal and set color variables accordingly

if [[ -t 1 ]] || [[ -t 2 ]]; then
    RED=$'\033[0;31m'
    GREEN=$'\033[0;32m'
    YELLOW=$'\033[1;33m'
    BLUE=$'\033[0;34m'
    CYAN=$'\033[0;36m'
    NC=$'\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    CYAN=''
    NC=''
fi

export RED GREEN YELLOW BLUE CYAN NC
