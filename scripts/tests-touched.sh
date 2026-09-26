#!/usr/bin/env bash
# Коммит, меняющий код, без изменения проверок — предупреждение, не отказ
# (PROMPT-TESTING.md, уровень 5).
#
# Правило проекта: проверка считается сделанной, когда показано, что она
# краснеет без починки. Коммит, трогающий internal/, cmd/ или web/src/
# и не трогающий ни одного теста, — повод посмотреть: либо изменение
# держит уже существующая проверка (и это стоит сказать в сообщении),
# либо его не держит ничто. Падать не должно: правке комментария или
# переименованию смотреть нечего.
#
# Использование: tests-touched.sh <от>..<до>
set -euo pipefail

range=${1:?нужен диапазон коммитов, например origin/master..HEAD}

warned=0
for c in $(git rev-list --no-merges "$range"); do
  files=$(git diff-tree --no-commit-id --name-only -r "$c")
  code=$(grep -E '^(internal|cmd)/.*\.go$|^web/src/.*\.(ts|tsx)$' <<<"$files" \
    | grep -vE '_test\.go$|\.test\.tsx?$|/i18n/' || true)
  tests=$(grep -E '_test\.go$|\.test\.tsx?$|^web/e2e(-stand)?/|^scripts/upgrade-check\.sh$' <<<"$files" || true)
  if [ -n "$code" ] && [ -z "$tests" ]; then
    subject=$(git log -1 --format=%s "$c")
    echo "::warning::${c:0:7} «$subject» меняет код и не трогает проверок: $(tr '\n' ' ' <<<"$code" | cut -c1-200)"
    warned=$((warned + 1))
  fi
done
echo "коммитов с кодом без проверок: $warned"
