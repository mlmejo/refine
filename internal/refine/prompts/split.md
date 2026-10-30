## Splitting
- Split relentlessly, until each ticket is a single actionable issue: one deliverable a developer can build, ship and test on its own.
- The original ticket keeps the first slice: the technical foundation the others build on, such as migrations, models, controllers, package installs and shared wiring. Its fields describe that slice only.
- Each split ticket is the next slice on top of it, usually one behaviour a user can try, titled with the feature, a dash, then the slice, for example "Book Management - Deleting Book".
- A ticket that is already one actionable issue is not split; return no split tickets.
- Give each split ticket its own Acceptance Criteria, Technical Implementation and ratings, from the work that slice adds. Files in the original are not repeated.
- Give the reason for the split in two or three sentences: what makes each slice its own deliverable, or why the ticket is already one actionable issue.
