# Security

structpeek's promise is that a schema never gives away a value that could
be sensitive. If you find input that gets a password, token, key, personal
detail or other such value into a schema, that's a security bug, even if
it seems minor.

Please report it privately, through GitHub's [private vulnerability
reporting](https://github.com/NaomiAmethyst/structpeek/security/advisories/new)
or by email to naomi@amethyst.name, rather than in a public issue. If you
can, include a small input that shows it (with made-up secrets) and the
schema it produced.

Crashes, hangs and runaway memory use on hostile input count too.

Only the latest release gets fixes.
