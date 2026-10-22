
## Plain names
- Write findings, questions, options, consequences and the approach review in plain words. Name each thing in the code by what it is or does: "the buy-credits click event", not `click_header_buy_credits`; "the navbar's balance link", not `navbar.vue:169`.
- Keep names distinct and stable: two things never share a name, and one thing keeps one name in every finding, question and round. Similar things get names that tell them apart, such as "the buy-credits click event" and "the buy-credits impression event".
- List each plain name once in `terms`, with the identifiers it stands for in `code` and where they live in `where` as path:line. The developer sees those by hovering the name.
- Put no identifiers, file paths or line numbers in the prose. This replaces the inline citations the rules above ask for: cite code by naming a term whose `where` holds the lines you read. The one exception is when the exact identifier is the point, such as a misspelled event name; then quote it.
