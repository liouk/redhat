# Summary

When removing a user from a group, `removeUserFromGroup` gets a pointer to the group object living in the informer cache via the lister, then builds the new user list using `append` on a sub-slice of it:

```go
newUsers = append(updatedGroup.Users[0:userIdx], updatedGroup.Users[userIdx+1:]...)
```

Because the sub-slice shares the backing array with the cached object, `append` mutates the cached object's user list in place — the removed user disappears and the last user gets duplicated. When the watch event later arrives and the informer re-indexes the group, it diffs the corrupted old object against the new one. Since the removed user is no longer in the corrupted old object, its index entry (user → group) is never cleaned up and becomes a permanent phantom.

On subsequent logins, `GroupsFor(user)` returns the phantom group from the index. `groupsDiff` sees that the cache already includes the group, matches the provider's desired state, and computes no changes needed — so the user is never actually added to the group.

### Fix

Allocate independent slices when building the new user list so the cached object is never mutated:

```go
case 0:
    newUsers = append([]string(nil), updatedGroup.Users[1:]...)
default:
    newUsers = make([]string, 0, len(updatedGroup.Users)-1)
    newUsers = append(newUsers, updatedGroup.Users[:userIdx]...)
    newUsers = append(newUsers, updatedGroup.Users[userIdx+1:]...)
```

## Starting state

At 10:40:37, `user7-123` is added to `group4-123`. The API update succeeds, the watch event arrives, and the cache correctly stores:

```
group4-123.Users = [user18, user14, user10, user3, user6, user7]
```

The `ByUser` index correctly maps `user7-123 → {group4-123, ...}`.

Everything is fine so far.

## The corruption (10:40:41) — caused by a different user

Four seconds later, a completely unrelated login happens: `user10-123` is being removed from `group4-123`. `removeUserFromGroup` calls `m.groupsLister.Get("group4-123")`, which returns a pointer directly into the cache:

```
cached Users slice: [user18, user14, user10, user3, user6, user7]
                     idx 0    idx 1    idx 2   idx 3  idx 4  idx 5
```

`user10` is at index 2. The old code runs:

```go
newUsers = append(Users[0:2], Users[3:]...)
```

This `append` has enough capacity in the existing backing array, so it shifts elements left in place:

```
before: [user18, user14, user10, user3, user6, user7]
after:  [user18, user14, user3,  user6, user7, user7]
                         ↑↑↑↑↑  ↑↑↑↑↑  ↑↑↑↑↑
                         shifted left by one position
```

`newUsers` gets a new slice header with `len=5`: `[user18, user14, user3, user6, user7]`. This is sent to the API. **Correct.**

But the cached object's slice header is unchanged — still `len=6`. So it now reads:

```
[user18, user14, user3, user6, user7, user7]
```

`user10` has vanished. `user7` is duplicated. But the index still has `user10 → {group4-123}` from when it was originally indexed.

## The broken re-index (10:40:41)

The API update triggers a `MODIFIED` watch event. The reflector calls `store.Update(newObj)`, which does:

```go
oldObject := c.items["group4-123"]          // the CORRUPTED cached object
c.items["group4-123"] = newObj              // replace with new object from API
c.index.updateIndices(oldObject, newObj)     // re-index
```

The re-indexer calls `ByUserIndexKeys` on both objects. The logs show exactly what it sees:

```
re-indexing group; users=[user18, user14, user3, user6, user7, user7]  ← oldObj (corrupted)
re-indexing group; users=[user18, user14, user3, user6, user7]         ← newObj (from API)
```

The re-indexer's logic is:

1. For each user in oldObj: remove this group from their index entry
2. For each user in newObj: add this group to their index entry

It removes and re-adds entries for `{user18, user14, user3, user6, user7}`. But `user10` is **not** in the corrupted oldObj, so the re-indexer doesn't know to clean up `user10 → {group4-123}`. That becomes a **phantom**.

As for `user7` — it appears in both old and new, so its index entry `user7 → {group4-123}` survives correctly. `user7` is still fine at this point.

## user7 is removed from group4-123 (10:41:44)

Later, `user7-123`'s provider removes `group4-123` from its list. `removeUserFromGroup` is called. The lister returns the cached object, which by now (after `user8` was added at 10:41:28) contains:

```
cached Users: [user18, user14, user3, user6, user7, user8]
               idx 0   idx 1   idx 2  idx 3  idx 4  idx 5
```

`user7` is at index 4. The old code runs:

```go
newUsers = append(Users[0:4], Users[5:]...)
```

`append` writes `user8` into position 4 of the same backing array:

```
before: [user18, user14, user3, user6, user7, user8]
after:  [user18, user14, user3, user6, user8, user8]
                                       ↑↑↑↑↑
                                       overwritten
```

The cached object (`len=6`) now reads `[user18, user14, user3, user6, user8, user8]`. `user7` has vanished from the cached object.

The API update succeeds with the correct `[user18, user14, user3, user6, user8]`. `user7-123` has been correctly removed from the group in the API.

## user7's index entry becomes a phantom (10:41:44)

The watch event arrives. The re-indexer sees:

```
re-indexing group; users=[user18, user14, user3, user6, user8, user8]  ← oldObj (corrupted, NO user7)
re-indexing group; users=[user18, user14, user3, user6, user8]         ← newObj (correct)
```

It removes and re-adds entries for `{user18, user14, user3, user6, user8}`. `user7` is **not** in the corrupted oldObj, so `user7-123 → {group4-123}` is never deleted from the index.

From now on, `GroupsFor("user7-123")` will always return `group4-123` among the results.

## The user-visible bug (10:42:37)

`user7-123` logs in again. The provider says `user7-123` should be in `[group12, group19, group2, group4]`. The `processGroups` function calls `GroupsFor("user7-123")` to find what groups the cache thinks `user7-123` is already in:

```
cacheGroups:    [group12, group19, group2, group4]  ← includes phantom group4
providerGroups: [group12, group19, group2, group4]
```

`groupsDiff(cacheGroups, providerGroups)` computes: `remove=0, add=0`. The cache and the provider agree — so the code does nothing.

But the actual API state is:

```
clientGroups: [group12, group19, group2]  ← group4 is NOT here
```

`user7-123` is never added to `group4-123` because the phantom index entry makes the code believe it's already there. The log confirms:

```
remove: 0; add: 0; providerGroups: [[group12 group19 group2 group4]];
cacheGroups: [[group12 group19 group2 group4]];
clientGroups: [[group12 group19 group2]]; cache==client? false
addGroupsAPI: [[group4]] ← this is what SHOULD happen, but doesn't
```

The `addGroupsAPI` field (computed from the real API, for debug purposes only) shows that `group4-123` should be added. But the actual code path uses `addGroups` (computed from the cache), which is empty.
