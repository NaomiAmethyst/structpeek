# Security

structpeek's promise is that, by default, a schema gives away nothing from
the data's values, and that even with `--enums` or `--ranges` it gives away
nothing that could be sensitive. If you find input that gets a value into
a default schema, or a password, token, key, personal detail or other such
value into one made with those options, that's a security bug, even if it
seems minor.

Please report it privately, through GitHub's [private vulnerability
reporting](https://github.com/NaomiAmethyst/structpeek/security/advisories/new)
or by email to naomi@amethyst.name, rather than in a public issue. If you
can, include a small input that shows it (with made-up secrets) and the
schema it produced.

Crashes, hangs and runaway memory use on hostile input count too.

Only the latest release gets fixes.
