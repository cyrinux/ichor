#!/usr/bin/env bash
# Boots an API 34 aosp_atd x86_64 emulator with the hosted runner's own Android SDK
# ($ANDROID_HOME: sdkmanager, avdmanager, emulator, adb), runs the given command against it,
# then shuts it down: android.yml's ui-smoke, without a third-party action. Needs /dev/kvm.
#
#   scripts/ci-emulator.sh scripts/ui-smoke.sh
set -euo pipefail

API="${EMULATOR_API:-34}"
IMAGE="system-images;android-${API};aosp_atd;x86_64"
SDK="${ANDROID_HOME:?ANDROID_HOME is not set}"
tools="$SDK/cmdline-tools/latest/bin"
export PATH="$SDK/emulator:$SDK/platform-tools:$tools:$PATH"

# One AVD directory for avdmanager and the emulator: they look in different defaults.
export ANDROID_AVD_HOME="${RUNNER_TEMP:-/tmp}/avd"
mkdir -p "$ANDROID_AVD_HOME"

yes | sdkmanager --licenses >/dev/null 2>&1 || true
sdkmanager --install emulator platform-tools "$IMAGE" | tail -3
echo no | avdmanager create avd --force -n smoke -k "$IMAGE" -p "$ANDROID_AVD_HOME/smoke.avd"
[[ -f "$ANDROID_AVD_HOME/smoke.ini" ]] || { echo "avdmanager made no smoke.ini in $ANDROID_AVD_HOME" >&2; ls -la "$ANDROID_AVD_HOME" >&2; exit 1; }

emulator -avd smoke -no-snapshot-save -no-window -gpu swiftshader_indirect -noaudio -no-boot-anim -camera-back none \
  >"${RUNNER_TEMP:-/tmp}/emulator.log" 2>&1 &
emulator_pid=$!
trap 'adb emu kill >/dev/null 2>&1 || kill "$emulator_pid" 2>/dev/null || true' EXIT

# Boot: the device, then sys.boot_completed, at most 5 minutes.
timeout 300 adb wait-for-device
deadline=$((SECONDS + 300))
until [[ "$(adb shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" == 1 ]]; do
  if (( SECONDS > deadline )) || ! kill -0 "$emulator_pid" 2>/dev/null; then
    echo "the emulator did not boot:" >&2
    tail -40 "${RUNNER_TEMP:-/tmp}/emulator.log" >&2
    exit 1
  fi
  sleep 2
done

# No animations: the test's waits then see the final frames.
for scale in window_animation_scale transition_animation_scale animator_duration_scale; do
  adb shell settings put global "$scale" 0
done
adb shell input keyevent 82 || true

"$@"
