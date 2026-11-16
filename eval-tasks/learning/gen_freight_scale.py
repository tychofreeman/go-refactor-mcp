"""Generates eval-tasks/testdata/freight-xxl: freight-xl's shapes scaled up
K times (five by default), so that each change reaches several times the
places: K times the carriers whose Ship methods are near-duplicates, the
truck methods that call last, the packages that read Road and Rail, and
the packages that call Dispatch. Also its hidden tests
(testdata/freight-xxl-*), eval-tasks/derivations-freight-xxl.json with
the tasks dispatch, routes, tail and run, and their reference batches in
eval-tasks/learning/refs. gen_freight.py and freight-xl are unchanged.

Run from the repository root: python3 eval-tasks/learning/gen_freight_scale.py
"""
import argparse
import json
import os
import shutil
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import gen_freight as g  # noqa: E402

ROOT = g.ROOT
MOD = os.path.join(ROOT, "freight-xxl")
TASKS = "eval-tasks/derivations-freight-xxl.json"
REFS = "eval-tasks/learning/refs"

# Truck's methods that call last, renamed with a suffix in each copy.
TRUCK_METHODS = ["Label", "Masked", "Tails", "Hint", "Short", "Track", "Badge", "Plate", "Same", "Codes"]

# Packages that read Road and Rail, after freight-xl's manifests and audit.
READER_PACKAGES = ["customs", "billing", "tracking", "claims", "invoices", "docks", "yards", "returns"]

# Packages that call Dispatch: freight-xl's schedules and depots, then more.
SCHEDULE_PACKAGES = ["schedules", "shifts", "rounds", "calendars", "rosters", "cycles", "slots", "terms"]
DEPOT_PACKAGES = ["depots", "hubs", "terminals", "stations", "wharves", "sidings", "bays", "gates"]

# What each of freight-xl's carriers logs and ships in the hidden dispatch
# test: the "to" of its log line, and what it appends.
TO = ["0042", "Y7", "LHR", "RTM", "RD-0042", "Y7", "LHR", "RTM", "Ann", "S-77"]
SHIPPED = ["Ord-2", "Ord-2 5kg", "Ord-2", "ord-2", "Ord-2/van", "ORD-2", "D-Ord-2", "Ord-2", "Ord-2", "5kg Ord-2"]


def suffix(i):
    return "" if i == 0 else str(i + 1)


def carrier_list(k):
    """The carriers, freight-xl's first and then numbered copies. A copy of
    Truck ships to the whole road code, so that last stays Truck's alone."""
    out = []
    for i in range(k):
        for typ, name, field, to, text in g.CARRIERS:
            if i and typ == "Truck":
                to = "s.Road"
            out.append((typ + suffix(i), name + suffix(i), field, to, text))
    return out


def truck_methods(k):
    """Truck's methods that call last, and k-1 copies with numbered names."""
    core = g.CORE
    block = core[core.index("// Label is"):core.index("func last(")]
    out = []
    for i in range(1, k):
        b = block
        for m in TRUCK_METHODS:
            b = b.replace(f"// {m} ", f"// {m}{suffix(i)} ").replace(f") {m}(", f") {m}{suffix(i)}(")
        out.append(b)
    return "".join(out)


def core(k):
    saved = g.CARRIERS
    g.CARRIERS = carrier_list(k)
    try:
        text = g.CORE.replace("%CARRIERS%", g.carriers())
    finally:
        g.CARRIERS = saved
    return text.replace("func last(", truck_methods(k) + "func last(")


def renamed(text, old, new):
    return text.replace(f"// Package {old} ", f"// Package {new} ").replace(f"package {old}\n", f"package {new}\n")


def dispatch_test(k):
    cs = carrier_list(k)
    logs, shipped = [], []
    for i in range(k):
        for j, (typ, name, field, to, text) in enumerate(g.CARRIERS):
            t = "RD-0042" if i and typ == "Truck" else TO[j]
            logs.append(f"{name}{suffix(i)} ship to {t}")
            shipped.append(f"{t}: {SHIPPED[j]}")
    quote = lambda xs: ", ".join(json.dumps(x) for x in xs)
    cases = "".join(f"\tcase *{c[0]}:\n\t\treturn c.Shipped\n" for c in cs)
    return f'''
package freight

import (
	"bytes"
	"log"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestAcceptDispatch(t *testing.T) {{
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	var shipped []string
	dispatch(&shipped, "cart", "dock", "ORD-9")
	if want := []string{{"dock: ORD-9"}}; !reflect.DeepEqual(shipped, want) {{
		t.Errorf("dispatch appended %q, want %q", shipped, want)
	}}
	if !strings.Contains(buf.String(), "cart ship to dock") {{
		t.Errorf("dispatch logged %q", buf.String())
	}}
	buf.Reset()
	cs := []Carrier{{{", ".join("&" + c[0] + "{}" for c in cs)}}}
	s := Shipment{{ID: "S-77", Sender: "Ann", Road: "RD-0042", Rail: "Y7", Air: "LHR", Sea: "RTM"}}
	Dispatch(Config{{Carriers: cs}}, []Shipment{{s}}, Order{{Ref: "Ord-2", Weight: 5}})
	for _, want := range []string{{{quote(logs)}}} {{
		if !strings.Contains(buf.String(), want+"\\n") {{
			t.Errorf("shipping logged %q, want %q in it", buf.String(), want)
		}}
	}}
	got := []string{{}}
	for _, c := range cs {{
		got = append(got, shippedBy(c)...)
	}}
	want := []string{{{quote(shipped)}}}
	if !reflect.DeepEqual(got, want) {{
		t.Errorf("shipped %q, want %q", got, want)
	}}
}}

func shippedBy(c Carrier) []string {{
	switch c := c.(type) {{
{cases}	}}
	return nil
}}
'''


def prompts(k):
    p = dict(g.PROMPTS)
    names = ", ".join(c[0] for c in g.CARRIERS[:-1]) + " and " + g.CARRIERS[-1][0]
    p["dispatch"] = (f"The Ship methods of the {10 * k} carriers in package freight ({names}, and their numbered "
                     f"copies Truck2 to Post{k}) do nearly the same thing. Make all {10 * k} call one package-level "
                     "function dispatch(shipped *[]string, mode, to, text string), which logs \"<mode> ship to <to>\" "
                     "with the standard logger and appends \"<to>: <text>\" to *shipped. What they ship and log must not change.")
    return p


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("-k", type=int, default=5, help="how many times freight-xl's carriers and uses")
    a = ap.parse_args()
    k = a.k
    if not 1 <= k <= len(SCHEDULE_PACKAGES):
        ap.error(f"-k is from 1 to {len(SCHEDULE_PACKAGES)}")
    shutil.rmtree(MOD, ignore_errors=True)
    g.write(os.path.join(MOD, "go.mod"), "module example.com/freight\n\ngo 1.22\n")
    g.write(os.path.join(MOD, "freight/freight.go"), core(k))
    g.write(os.path.join(MOD, "freight/freight_test.go"), g.CORE_TEST)
    readers = "".join(g.READERS.replace("{F}", f).replace("{f}", f.lower()) for f in ("Road", "Rail"))
    g.write(os.path.join(MOD, "manifests/manifests.go"), g.MANIFESTS + readers)
    g.write(os.path.join(MOD, "audit/audit.go"), g.AUDIT + readers.replace("Missing", "Lacking").replace("Region", "Zone"))
    for pkg, text in g.READER_TESTS.items():
        g.write(os.path.join(MOD, pkg, pkg + "_test.go"), text)
    for p in READER_PACKAGES[:2 * k - 2]:
        g.write(os.path.join(MOD, p, p + ".go"), renamed(g.AUDIT, "audit", p) + readers)
        g.write(os.path.join(MOD, p, p + "_test.go"), renamed(g.READER_TESTS["audit"], "audit", p))
    for p in SCHEDULE_PACKAGES[:k]:
        g.write(os.path.join(MOD, p, p + ".go"), renamed(g.schedules(), "schedules", p))
        g.write(os.path.join(MOD, p, p + "_test.go"), renamed(g.SCHEDULES_TEST, "schedules", p))
    for p in DEPOT_PACKAGES[:k]:
        g.write(os.path.join(MOD, p, p + ".go"), renamed(g.depots(), "depots", p))
        g.write(os.path.join(MOD, p, p + "_test.go"), renamed(g.DEPOTS_TEST, "depots", p))
    hidden = dict(g.HIDDEN, dispatch=dispatch_test(k))
    text = prompts(k)
    tasks = []
    for name in ("dispatch", "routes", "tail", "run"):
        tests = os.path.join(ROOT, "freight-xxl-" + name)
        shutil.rmtree(tests, ignore_errors=True)
        g.write(os.path.join(tests, "freight", name + "_test.go"), hidden[name])
        tasks.append({
            "name": "freight-xxl-" + name,
            "dir": "testdata/freight-xxl",
            "tests": "testdata/freight-xxl-" + name,
            "check": "go vet ./... && go test ./...",
            "prompt": text[name],
        })
        # freight-xl's reference batch carries the change to every place;
        # its copy names tools as they are published now.
        with open(os.path.join(REFS, "freight-" + name + ".json")) as f:
            ref = f.read().replace('"inline_function"', '"inline"')
        with open(os.path.join(REFS, "freight-xxl-" + name + ".json"), "w") as f:
            f.write(ref)
    with open(TASKS, "w") as f:
        json.dump(tasks, f, indent=2)
        f.write("\n")
    subprocess.run(["gofmt", "-w", MOD] + [os.path.join(ROOT, "freight-xxl-" + n) for n in ("dispatch", "routes", "tail", "run")], check=True)


if __name__ == "__main__":
    main()
