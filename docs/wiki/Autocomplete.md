# Autocomplete

<!-- Linked screenshots and expandable previews require these HTML elements. -->
<!-- markdownlint-configure-file {"MD033": {"allowed_elements": ["a", "code", "details", "img", "summary"]}} -->

The query box suggests as you type: operators from the first letter, then each operator's values. ↑ ↓ move through
the suggestions, Tab inserts one, and Esc closes them. While operators are suggested, `?` opens Help; among an
operator's values it is typed like any other character, since globs (`f:*.ts?`) and regexes use it.

## Operators

From the first letter, the box lists the matching operators; a short name (`a`, `sym`) lists its operator first. Each
shows a short title, then a line on what it does: `struct:` reads **Structural search**, then "Code shaped like a
template; each :[hole] takes any code."

<!-- screen:operator-suggestions -->
**Screen 5: Autocomplete: operators** · Screen id `operator-suggestions` · milestone M2

<a href="images/mocks/OperatorSuggest.png">
<img src="images/mocks/OperatorSuggest.png" alt="Mock 5: Autocomplete: operators" width="900">
</a>

<details>
<summary>The built panel</summary>

<img
  src="https://raw.githubusercontent.com/cruisinme30/spelunk/main/docs/dev/proof/operator-suggestions.png"
  alt="Built panel for operator-suggestions"
  width="900">

</details>

<!-- /screen -->

## Values

After an operator, the box lists up to 10 of its values: authors with commit counts and when they last committed,
repos with their index state, languages, time windows with how many files changed, file types and folders, symbol
names, commit-message words.

Inserted values preserve literal backslashes and quotes. Values containing whitespace or beginning with `/` are
quoted so accepting a suggestion cannot turn it into a regex or consume the next query term.

### Authors

<!-- screen:value-suggestions -->
**Screen 6: Autocomplete: author values** · Screen id `value-suggestions` · milestone M3

<a href="images/mocks/ValueSuggest.png">
<img src="images/mocks/ValueSuggest.png" alt="Mock 6: Autocomplete: author values" width="900">
</a>

<details>
<summary>The built panel</summary>

<img
  src="https://raw.githubusercontent.com/cruisinme30/spelunk/main/docs/dev/proof/value-suggestions.png"
  alt="Built panel for value-suggestions"
  width="900">

</details>

<!-- /screen -->

<!-- screen:author-values -->
**Screen 19: Values: every author for `author:`** · Screen id `author-values` · milestone M3

<a href="images/mocks/AuthorValues.png">
<img src="images/mocks/AuthorValues.png" alt="Mock 19: Values: every author for author:" width="900">
</a>

<details>
<summary>The built panel</summary>

<img
  src="https://raw.githubusercontent.com/cruisinme30/spelunk/main/docs/dev/proof/author-values.png"
  alt="Built panel for author-values"
  width="900">

</details>

<!-- /screen -->

### Time windows

<!-- screen:since-values -->
**Screen 20: Values: time windows for `since:`** · Screen id `since-values` · milestone M2

<a href="images/mocks/SinceValues.png">
<img src="images/mocks/SinceValues.png" alt="Mock 20: Values: time windows for since:" width="900">
</a>

<details>
<summary>The built panel</summary>

<img
  src="https://raw.githubusercontent.com/cruisinme30/spelunk/main/docs/dev/proof/since-values.png"
  alt="Built panel for since-values"
  width="900">

</details>

A number lists each unit it can take (`3` offers `3min`, `3h` … `3y`). A whole window comes first, read as it
parses: `m` is months, so `since:6m` lists `6m` "Last 6 months" ahead of `6min` "Last 6 minutes".

`until:` offers the same windows, each saying when it ends instead of when it starts: `2w` reads "More than 2 weeks
ago · Before Sat, Sep 19", with how many files haven't changed since, under the headings Calendar days, Hours ago and
Longer ago. Neither suggests dates; type one (`2026-09-30` or `2026-09`) and it's checked as you go.

Each window counts the files the search would keep, as [`since:`](Commits-and-Time#since) decides: in Git a file
changed when it was last committed, or now when it has uncommitted changes, and elsewhere when it was last saved.

<!-- /screen -->

### File types and folders

<!-- screen:path-values -->
**Screen 21: Values: file types and folders for `f:`** · Screen id `path-values` · milestone M2

<a href="images/mocks/PathValues.png">
<img src="images/mocks/PathValues.png" alt="Mock 21: Values: file types and folders for f:" width="900">
</a>

<details>
<summary>The built panel</summary>

<img
  src="https://raw.githubusercontent.com/cruisinme30/spelunk/main/docs/dev/proof/path-values.png"
  alt="Built panel for path-values"
  width="900">

</details>

<!-- /screen -->

### Every other operator

<!-- screen:other-values -->
**Screen 22: Values: `repo:`, `lang:`, `type:`, `kind:`, `case:`, `word:`, `count:`, `order:`, `is:`, `sym:`, `msg:`** ·
Screen id `other-values` · milestone M2

One board on the canvas, switched by its operator tweak. Each operator's values:

#### `repo:`

<a href="images/mocks/OtherValues-repo.png">
<img src="images/mocks/OtherValues-repo.png" alt="Mock 22: values for repo:" width="900">
</a>

#### `lang:`

<a href="images/mocks/OtherValues-lang.png">
<img src="images/mocks/OtherValues-lang.png" alt="Mock 22: values for lang:" width="900">
</a>

<details>
<summary>The built panel for <code>lang:</code>
</summary>

<img
  src="https://raw.githubusercontent.com/cruisinme30/spelunk/main/docs/dev/proof/other-values.png"
  alt="Built panel for other-values"
  width="900">

</details>

#### `type:`

<a href="images/mocks/OtherValues-type.png">
<img src="images/mocks/OtherValues-type.png" alt="Mock 22: values for type:" width="900">
</a>

#### `kind:`

`kind:` offers its six kinds, each with what it covers.

<a href="images/mocks/OtherValues-kind.png">
<img src="images/mocks/OtherValues-kind.png" alt="Mock 22: values for kind:" width="900">
</a>

#### `case:`

<a href="images/mocks/OtherValues-case.png">
<img src="images/mocks/OtherValues-case.png" alt="Mock 22: values for case:" width="900">
</a>

#### `word:`

<a href="images/mocks/OtherValues-word.png">
<img src="images/mocks/OtherValues-word.png" alt="Mock 22: values for word:" width="900">
</a>

#### `count:`

<a href="images/mocks/OtherValues-count.png">
<img src="images/mocks/OtherValues-count.png" alt="Mock 22: values for count:" width="900">
</a>

#### `order:`

<a href="images/mocks/OtherValues-order.png">
<img src="images/mocks/OtherValues-order.png" alt="Mock 22: values for order:" width="900">
</a>

#### `is:`

Each file state says how many files are in it now, and names the open and uncommitted ones; `test`, `generated` and
`vendored` say what counts instead. Outside Git, `changed` says "No Git repo".

<a href="images/mocks/OtherValues-is.png">
<img src="images/mocks/OtherValues-is.png" alt="Mock 22: values for is:" width="900">
</a>

#### `owner:`

Each owner the open repos' CODEOWNERS files name, most files first, says whether it is a team, a person or an email,
how many files it owns and in which repos; `none` comes last, with how many files have no owner. What you type
narrows them, with or without the `@`. With no CODEOWNERS file open there is nothing to offer.

<img
  src="https://raw.githubusercontent.com/cruisinme30/spelunk/main/docs/dev/proof/owner-values.png"
  alt="Built panel: values for owner:"
  width="900">

#### `at:`

The open repos' tags come first, newest first, each with the day it was made and the repos that have it; then times
(`yesterday`, `2h`, `2w`, `6m`, `1y` …), each saying when it is: "Files as they were on Fri, Apr 10". What you type
narrows the tags anywhere in their name, and the times by how they start.

<img
  src="https://raw.githubusercontent.com/cruisinme30/spelunk/main/docs/dev/proof/past-revision-values.png"
  alt="Built panel: values for at:"
  width="900">

#### `sym:`

<a href="images/mocks/OtherValues-sym.png">
<img src="images/mocks/OtherValues-sym.png" alt="Mock 22: values for sym:" width="900">
</a>

`ref:` offers the same definition names.

#### `msg:`

<a href="images/mocks/OtherValues-msg.png">
<img src="images/mocks/OtherValues-msg.png" alt="Mock 22: values for msg:" width="900">
</a>

<!-- /screen -->

---

**Next:** [[Settings and Commands]]: every setting and command.
