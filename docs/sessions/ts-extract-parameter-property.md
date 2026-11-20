# extract_function and properties taken for locals

- **Item:** issues #711 and #725 (a constructor parameter property,
  `this.s` or `bed.names`, written as a bare name) and #723 (a write to
  `bed.names` taken for an assignment of `bed`, so the function returned
  `bed` and the call reassigned it), filed by sessions that built
  TypeScript toys through the tools.
- **Date:** 2026-11-20
- **Branch:** ts-extract-parameter-property

## Plan

At the highest level: extract_function's analysis of the statements'
locals reads a property access as a use or write of a variable.
- **The easy change:** in the loop that collects locals, skip a name
  that is a property (`propertyName`): a parameter property's symbol is
  declared by the constructor's parameter, so it passed as a local. In
  `assignedIn`, ask which names an assignment writes (`assigned`, as
  `Assigns` does), not every name in its target.
- **The places today:** those two; one each.
- **The refactorings:** none.

## Notes

- refusal: replace_expression of the right half of an `&&` chain found
  no such expression (the chain groups to the left); replacing the whole
  condition did it.

## Measured

Four edit steps (add_element of the two cases, two replace_expression);
nothing by hand; one refusal, above.

## Assessment

Through the tools; two conditions changed. Extracting a mutating
statement then function_to_method now works, since the function no
longer assigns its parameter (#723's follow-on).

## Follow-ups

- #724 (inline of the function the old extraction wrote) is its own
  issue.
