# Scratch Repository

This repository is mainly for use by people learning how to use Gerrit and
contribute to Go.

[Click here for a tutorial][gophercon-tutorial] on how to get started with a
contribution to this repository.

A fuller, text-based tutorial based around the [core Go project can be found
here][core-go-tutorial].

## What should I add?

Add a folder with your username, and put a main function in there. You can
put whatever you want in your main function; see the existing directories for
examples.

All files should have the standard licensing header, and add appropriate
documentation see the other files in this repository for an example.

## Notes about Gerrit

If you have needed to change a Github pull request, you probably just added a
second commit with the requested changes and pushed it. By contrast, all changes
opened in Gerrit are a single commit, which means you need to [amend your
commit][amend] if the reviewer requests feedback.

[amend]: http://www.joinfu.com/2013/06/pushing-revisions-to-a-gerrit-code-review/

To amend a previous commit, run `git add (list of files you changed)` to add
your changes, then run `git commit --amend` to amend the commit to add new
data. Your commit message should still summarize the entire commit ("Add
kevinburke/main.go"), not just the change a reviewer asked for ("Fix typo").

After you amend the commit, re-run `git codereview mail` to push that change
to the server. Then in the Gerrit UI, find the in-line comments left by your
reviewer, click "Done", go back to the main PR page, and click "Reply" => "Send"
to tell your reviewer that you've addressed your feedback.

Once you get a "Code-Review: +2" from a Go contributor, your change will be
merged!

## Need help?

Gerrit is not easy to get started with, and we want to help you out. If you are
having trouble with Gerrit, contact the [golang-devexp][devexp] mailing list for
help!

[gophercon-tutorial]: https://golang.org/s/gophercon2017
[core-go-tutorial]: https://golang.org/doc/contribute.html
[devexp]: https://groups.google.com/forum/#!forum/golang-devexp
