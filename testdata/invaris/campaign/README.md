# Campaign verification fixture

`behavior.sh`, `test.sh`, `validate.sh`, `killed.json` and `survived.json` are
copied from Invaris `examples/mutation`. `accept.json` runs the killed mutant
only (ACCEPT); `reject.json` adds the surviving mutant, which
`requireNoSurvivors` rejects (REJECT). The scripts form the imported tree; the
campaign and harness JSON files are the frozen verification files.
