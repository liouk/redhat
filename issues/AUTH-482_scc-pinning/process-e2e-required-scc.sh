#!/usr/bin/env bash
input_file="$1"
grep required-scc $input_file | grep -v "<system-out>" | grep -v "Jira" > results.md
sed -i 's/\s*    <testcase name="\[sig-auth\] all workloads in ns\//#### /g' results.md
sed -i 's/ must set the &#39;openshift.io\/required-scc&#39; annotation" time="0"><\/testcase>//g' results.md
sed -i 's/ must set the &#39;openshift.io\/required-scc&#39; annotation" time="0">/\n| pod | scc | owner |\n| --- | --- | --- |/g' results.md
sed -i 's/\s*<failure message="">//g' results.md
sed -i 's/&#xA;/ |\n/g' results.md
sed -i 's/^annotation missing from pod &#39;/| /g' results.md
sed -i 's/&#39;; suggested required-scc: &#39;/ | /g' results.md
sed -i 's/&#39;; owners: / | /g' results.md
sed -i 's/<\/failure>/ |/g' results.md

cp results.md "$HOME/Syncthing/notes/4 RED HAT/Work/"
