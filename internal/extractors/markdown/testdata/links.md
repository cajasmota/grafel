# Links Doc

This page links to [a sibling](./sibling.md) and a [parent](../parent.md).

It also references [an absolute](/root.md) page and an [external site](https://example.com)
plus a [mail link](mailto:foo@bar.com) and an [in-page anchor](#links-doc).

## Section with [inline link](./sibling.md#anchor)

Duplicate link to [sibling again](./sibling.md) should dedupe.

```markdown
[link in code block](./should-be-ignored.md)
```

After the fence, [post-fence](./after.md) is real.
