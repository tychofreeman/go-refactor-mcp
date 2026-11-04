# inline_struct

- **Item:** roadmap 51.3 (2026-11-04), with the user's decisions:
  a separate tool; `elsewhere` (refuse, or keep the struct for its other
  uses); methods move, with a check; `prefer` (outer or inner) for a
  method both have; a field both have always refused; Go first.
- **Date:** 2026-11-04
- **Branch:** inline-struct (from extract-interface, #545)

## Plan

At the level of the operation: the inverse of extract_struct, so an
extraction can be undone the way inline_function undoes
extract_function. Every use of the struct goes through the holder's
field, so the change is the selector `.Soil` dropped, the literal's
inner braces dropped, and the receivers renamed; what can tell the
difference is a use of the field as a whole, the method set (for a named
field), and reflection.

Derivation, in TDD's order: no reshaping; thirteen goldens and the test
by create_file (`New` stubbed); the code by add_declaration, registered
by add_element.

## Notes

- The goldens passed on the first run; the review of each output found
  one choice to keep: moved methods keep their receiver's name (`s`), as
  function_to_method does.
- Checking the method set comes before rewriting uses: otherwise a named
  field printed as a whole (`fmt.Println(b.soil)`) is refused as a use
  of the whole before the method that would change what it prints.
- The instructions list the tool; 13 characters cut from the
  create_file line keep them under 6,000.
- Stress, with the package's tests after each (elsewhere keep): x/tools 4
  of 118 applied, no failure or rollback; uritemplate, jsonschema-go and
  text/template/parse none of 8. Refused most: the struct declared in
  another file than its holder (a candidate to lift: the edits already
  reach every file), a field used as a whole, methods that use their
  receiver as a whole, and literals giving the field a variable.
- By hand: nothing.
