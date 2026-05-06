# >>> i3-notch >>>
# Managed by i3-notch installer. Do not edit between markers.
polybar notch 2>&1 | tee -a /tmp/polybar-notch.log & disown
pkill -f raise-notch.sh 2>/dev/null
nohup __I3NOTCH_ROOT__/scripts/raise-notch.sh >/tmp/raise-notch.log 2>&1 & disown
# <<< i3-notch <<<
