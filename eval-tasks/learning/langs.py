"""The languages the planning loop runs on: what it calls the module and
the tools, how it runs a batch and the module's tests, how it shows the
module to the plan, and how it finds a place a message names.

Go is gorefactor, from this checkout, and so is TypeScript: the same
tools, which go by a file's extension. Java is java-refactor-mcp
(tychofreeman/java-refactor-mcp), from a checkout given with -server: its
bin/jrefactor runs a tool from a shell, with the arguments and batch form
gorefactor's CLI takes.
"""
import json
import os
import re
import subprocess

import probe


class Lang:
    """What the loop needs to know of one language and its server."""

    name = ""
    module = ""  # what the plan calls the code: "a Go module"
    server = ""  # the tools' name in the prompts
    names = ""  # how a tool's name argument names a declaration
    place = None  # a file:line a message names
    check = ""  # the module's own checks, a shell command run in it
    head = ""  # what introduces view's output in the plan; the loop's HEADS when empty
    declaration = re.compile(r"(func|type|var|const) ")  # starts a top-level declaration

    def cli(self):
        """The command that runs one tool on a module: argv before -C."""
        raise NotImplementedError

    def tool_list(self):
        """The server's tools/list: name, description and input schema of each."""
        raise NotImplementedError

    def view(self, module):
        """The module as the plan sees it: names and signatures, no bodies."""
        raise NotImplementedError

    def batch(self, module, path):
        """Runs the batch in the file path on module. Returns (applied,
        output), the output of a refusal starting "REFUSED batch at step N
        of M (tool): ", as gorefactor writes it."""
        r = probe.sh(self.cli() + ["-C", module, "batch", "@" + path])
        out = (r.stdout + r.stderr).strip()
        return out.startswith("APPLIED"), out

    def places(self, module, text):
        """The (file, line) pairs text names, file relative to module."""
        return [(f, int(n)) for f, n in self.place.findall(text)]

    def around(self, lines, line):
        """The first and last line of the declaration around line, 1-based."""
        start = line
        while start > 1 and not self.declaration.match(lines[start - 1]):
            start -= 1
        end = line
        while end < len(lines) and lines[end - 1] != "}" and not (end == start and lines[end - 1].endswith("}")):
            end += 1
        return start, end


class Go(Lang):
    name = "go"
    module = "a Go module"
    server = "gorefactor"
    names = "Where a tool takes file and name without line, name is a package-level symbol of file's package (Name, Type.Method or Type.Field), and stays valid after earlier steps."
    place = re.compile(r"([\w./-]+\.go):(\d+)")
    check = "go vet ./... && go test ./..."

    def __init__(self, tmp):
        self.binary = os.path.join(tmp, "refactor")
        probe.sh(["go", "build", "-o", self.binary, "./cmd/refactor"])

    def cli(self):
        return [self.binary]

    def tool_list(self):
        return probe.tool_list()

    def view(self, module):
        return probe.view("calltree", module)


class TypeScript(Go):
    name = "typescript"
    module = "a TypeScript project"
    names = "Where a tool takes file and name without line, name is a top-level symbol of file (name, Class.method or Class.field), and stays valid after earlier steps; a file is a path from the project's root (src/freight.ts)."
    # path.ts:12, and tsc's path.ts(12,5).
    place = re.compile(r"([\w./-]+\.tsx?)(?::|\()(\d+)")
    check = "tsc -p . && node --test"
    head = "The project: every file's path, then each source file but the tests without its function bodies (names, signatures, fields and doc comments), with line numbers:"
    declaration = re.compile(r"(export|function|class|interface|type|const|let|enum|abstract|declare) ")

    def tool_list(self):
        # In a TypeScript project the server describes the tools for TypeScript.
        return probe.tool_list(files=["tsconfig.json"])

    def view(self, module):
        files = sorted(os.path.relpath(os.path.join(d, f), module)
                       for d, _, fs in os.walk(module) if "node_modules" not in d
                       for f in fs if f.endswith((".ts", ".tsx")))
        parts = ["Files: " + ", ".join(files)]
        for f in files:
            if not f.endswith((".test.ts", ".test.tsx")):
                r = probe.sh(self.cli() + ["-C", module, "inspect", "file=" + f])
                parts.append(r.stdout.strip())
        return "\n\n".join(parts)

    def places(self, module, text):
        """The (file, line) pairs text names; node's stack traces name a
        file by its URL, which is given from module on."""
        text = text.replace("file://", "").replace(os.path.realpath(module) + os.sep, "").replace(module + os.sep, "")
        return [(f, int(n)) for f, n in self.place.findall(text)]


class Java(Lang):
    name = "java"
    module = "a Java project (Maven)"
    server = "jrefactor"
    names = "Where a tool takes name, it is Type.member (Truck.label, Shipment.road), or Type for a type, and stays valid after earlier steps; a file is a path from the project's root (src/main/java/freight/Truck.java)."
    # path.java:12, path.java line 12, path.java, lines 12-14; and line 12 of Name.java.
    place = re.compile(r"([\w./-]+\.java)(?::|,? lines? )(\d+)|lines? (\d+)(?:-\d+)? of ([\w./-]+\.java)")
    check = "./mvnw -q -o test"
    head = "The project: every file's path, then each main source file without its method bodies (names, signatures, fields and doc comments), with line numbers:"

    def __init__(self, checkout):
        self.checkout = os.path.abspath(checkout)
        self.jrefactor = os.path.join(self.checkout, "bin", "jrefactor")
        probe.sh([self.jrefactor, "list"])  # builds the jar when it is stale

    def cli(self):
        return [self.jrefactor]

    def tool_list(self):
        jar = os.path.join(self.checkout, "target", "java-refactor-mcp.jar")
        return probe.tool_list(["java", "-jar", jar, self.checkout])

    def view(self, module):
        files = sorted(os.path.relpath(os.path.join(d, f), module)
                       for d, _, fs in os.walk(module) for f in fs if f.endswith(".java"))
        parts = ["Files: " + ", ".join(files)]
        for f in files:
            if f.startswith("src/main/"):
                r = probe.sh(self.cli() + ["-C", module, "inspect", "file=" + f])
                parts.append(r.stdout.strip())
        return "\n\n".join(parts)

    def batch(self, module, path):
        r = probe.sh(self.cli() + ["-C", module, "batch", "@" + path])
        out = (r.stdout + r.stderr).strip()
        if r.returncode == 0:
            return True, out
        # refused: step 3 of 4 (inline) refused: why ... -> gorefactor's form.
        m = re.match(r"refused: step (\d+) of (\d+) \((\w+)\) refused: ", out)
        if m:
            out = f"REFUSED batch at step {m.group(1)} of {m.group(2)} ({m.group(3)}): " + out[m.end():]
        elif out.startswith("refused: "):
            out = "REFUSED batch: " + out[len("refused: "):]
        return False, out

    def places(self, module, text):
        """The (file, line) pairs text names; a file named by its name alone
        (Truck.java) is the one file of that name under module."""
        out = []
        text = re.sub(r"\.java:\[(\d+),\d+\]", r".java:\1", text)  # Maven's file.java:[16,29]
        for path, n, n2, path2 in self.place.findall(text):
            path, n = (path, n) if path else (path2, n2)
            if (i := path.find("src/main/java/")) > 0 or (i := path.find("src/test/java/")) > 0:
                path = path[i:]  # a copy's absolute path, from the source root on
            if "/" not in path:
                found = [os.path.relpath(os.path.join(d, path), module)
                         for d, _, fs in os.walk(module) if path in fs]
                if len(found) != 1:
                    continue
                path = found[0]
            out.append((path, int(n)))
        return out

    def around(self, lines, line):
        """Java has no top-level marker a line scan can trust: the lines
        around line, eight each side."""
        return max(1, line - 8), min(len(lines), line + 8)


def of(name, tmp, checkout):
    if name == "java":
        if not checkout:
            raise SystemExit("-lang java needs -server, the java-refactor-mcp checkout")
        return Java(checkout)
    return TypeScript(tmp) if name == "typescript" else Go(tmp)
