// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import "testing"

// FuzzDecode checks no input makes any decoder panic or hang.
func FuzzDecode(f *testing.F) {
	for _, seed := range []string{
		`{"a": [1, "x", null, {"b": true}]}`, "{\"a\":1}\n{\"a\":2}\n",
		"a: &x [1, 2]\nb: *x\n---\n- c\n", "<<: {a: 1}\nb: 2\n",
		"t = 1979-05-27T07:32:00Z\n[s]\nk = \"v\"\n[[a]]\nn = 1\n",
		"g = 1\n[s]\nk = v ; c\nk = w\nflag\n",
		"a,b\n1,\"x,y\"\n2,\n", "1\t2\n3\t4\n",
		`<a x="1"><b>t</b>text<c/><!-- c --><![CDATA[d]]></a>`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, format := range append(Formats, Auto) {
			Decode(data, format, Options{CSVHeader: HeaderAuto})
		}
	})
}
