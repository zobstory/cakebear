package main

// workspaceReplies is what Node 24.18 served for examples/workspace, run
// from a copy of it with testdata/paths-hook.mjs for its @/ alias, 5
// requests each (~/Desktop/cakebear/spikes/m7-step5/gen_workspace_replies.py,
// which writes this file).
var workspaceReplies = []struct {
	path, contentType, body string
	status                  int
}{
	{"/", "text/html; charset=utf-8", "<!doctype html>\n<title>Three days</title>\n<ul>\n  <li><a href=\"/mon\">Mon: sun, 21\u00b0 / 12\u00b0</a></li>\n  <li><a href=\"/tue\">Tue: rain &amp; wind, 18\u00b0 / 11\u00b0</a></li>\n  <li><a href=\"/wed\">Wed: cloud, 23\u00b0 / 14\u00b0</a></li>\n</ul>\n", 200},
	{"/mon", "text/plain; charset=utf-8", "Mon: sun, 21\u00b0 / 12\u00b0\n", 200},
	{"/tue", "text/plain; charset=utf-8", "Tue: rain & wind, 18\u00b0 / 11\u00b0\n", 200},
	{"/wed", "text/plain; charset=utf-8", "Wed: cloud, 23\u00b0 / 14\u00b0\n", 200},
	{"/thu", "text/plain; charset=utf-8", "not found\n", 404},
	{"/Mon", "text/plain; charset=utf-8", "not found\n", 404},
}
