/-
Formal model of Loom's Strategic Merge Patch semantics (specs/smp.md).

This is a proof-of-concept: it models the merge rules B1-B4 and B6 over a
simplified YAML value type and proves the spec's claims as theorems. B5
(map-list merge by inferred key) and B7/B8 (templating, error propagation)
are out of scope for the PoC.

The Go implementation is NOT verified by these proofs — they verify that the
*specification* is coherent (e.g. B4's append-unique really is idempotent).
Binding the Go code to this model requires differential testing against an
oracle extracted from these definitions.
-/

namespace LoomSpec

/-! ## B4: scalar list append-unique

`addNew target patch` appends each patch element not already present,
left to right. This is the spec's "union — target values first, then new
patch values appended", with duplicate patch values suppressed because a
value appended once is present for every later check. -/

def addNew (target : List String) : List String → List String
  | [] => target
  | x :: xs => if x ∈ target then addNew target xs else addNew (target ++ [x]) xs

/-- B4 order: the target list is preserved as a prefix of the result. -/
theorem addNew_prefix (t p : List String) : ∃ rest, addNew t p = t ++ rest := by
  induction p generalizing t with
  | nil => exact ⟨[], by simp [addNew]⟩
  | cons x xs ih =>
    by_cases hx : x ∈ t
    · simpa [addNew, hx] using ih t
    · obtain ⟨rest, hrest⟩ := ih (t ++ [x])
      exact ⟨[x] ++ rest, by simp [addNew, hx, hrest]⟩

/-- Every target element survives the merge. -/
theorem mem_addNew_of_mem_target {x : String} (t p : List String)
    (hx : x ∈ t) : x ∈ addNew t p := by
  obtain ⟨rest, hrest⟩ := addNew_prefix t p
  rw [hrest]; exact List.mem_append_left _ hx

/-- Every patch element is present in the merge (union, ⊇ patch). -/
theorem mem_addNew_of_mem_patch {x : String} (t p : List String)
    (hx : x ∈ p) : x ∈ addNew t p := by
  induction p generalizing t with
  | nil => cases hx
  | cons y ys ih =>
    by_cases hy : y ∈ t
    · rw [addNew, if_pos hy]
      rcases List.mem_cons.mp hx with rfl | hxs
      · exact mem_addNew_of_mem_target t ys hy
      · exact ih t hxs
    · rw [addNew, if_neg hy]
      rcases List.mem_cons.mp hx with rfl | hxs
      · exact mem_addNew_of_mem_target _ ys (by simp)
      · exact ih (t ++ [y]) hxs

/-- No values are invented: the merge contains only target or patch values. -/
theorem mem_addNew_iff {x : String} (t p : List String) :
    x ∈ addNew t p ↔ x ∈ t ∨ x ∈ p := by
  constructor
  · intro hx
    induction p generalizing t with
    | nil => exact .inl hx
    | cons y ys ih =>
      by_cases hy : y ∈ t
      · rw [addNew, if_pos hy] at hx
        rcases ih t hx with h | h
        · exact .inl h
        · exact .inr (List.mem_cons_of_mem _ h)
      · rw [addNew, if_neg hy] at hx
        rcases ih (t ++ [y]) hx with h | h
        · rcases List.mem_append.mp h with h | h
          · exact .inl h
          · simp at h; exact .inr (by simp [h])
        · exact .inr (List.mem_cons_of_mem _ h)
  · rintro (h | h)
    · exact mem_addNew_of_mem_target t p h
    · exact mem_addNew_of_mem_patch t p h

/-- If every patch value already exists in the target, nothing changes. -/
theorem addNew_of_subset (t p : List String) (h : ∀ x ∈ p, x ∈ t) :
    addNew t p = t := by
  induction p with
  | nil => rfl
  | cons x xs ih =>
    rw [addNew, if_pos (h x (by simp))]
    exact ih fun y hy => h y (List.mem_cons_of_mem _ hy)

/-- **B4 idempotence**: applying the same patch twice equals applying it
once. This is the property that makes Loom patches safe to re-run. -/
theorem addNew_idem (t p : List String) :
    addNew (addNew t p) p = addNew t p :=
  addNew_of_subset _ _ fun _ hx => mem_addNew_of_mem_patch t p hx

/-- B4 dedup: merging never introduces a duplicate. -/
theorem addNew_nodup (t p : List String) (ht : t.Nodup) :
    (addNew t p).Nodup := by
  induction p generalizing t with
  | nil => exact ht
  | cons x xs ih =>
    by_cases hx : x ∈ t
    · rw [addNew, if_pos hx]; exact ih t ht
    · rw [addNew, if_neg hx]
      refine ih (t ++ [x]) ?_
      simp [List.nodup_append, ht]
      exact fun a ha h => hx (h ▸ ha)

/-! ## B1/B2/B3/B6: deep merge over YAML documents

Simplified YAML: scalars, scalar lists, and maps (association lists with
unique keys, which loom's YAML parsing guarantees). -/

inductive Yaml where
  | scalar (s : String)
  | strList (xs : List String)
  | map (kvs : List (String × Yaml))
deriving Repr

/-- Association list lookup. -/
def find (k : String) : List (String × Yaml) → Option Yaml
  | [] => none
  | (k', v) :: rest => if k' = k then some v else find k rest

/-- Patch entries whose key is absent from the target (B6 candidates). -/
def onlyNew (t : List (String × Yaml)) : List (String × Yaml) → List (String × Yaml)
  | [] => []
  | (k, pv) :: rest =>
    if (find k t).isNone then (k, pv) :: onlyNew t rest else onlyNew t rest

mutual
/-- `merge target patch` per specs/smp.md. The patch drives the recursion:
- both scalar lists → B4 append-unique
- both maps         → recursive merge (B3)
- anything else     → patch replaces target (B1) -/
def merge : Yaml → Yaml → Yaml
  | .strList t, .strList p => .strList (addNew t p)
  | .map t, .map p => .map (mergeMap t p)
  | _, p => p

/-- Merge map entries: target entries keep their order and are deep-merged
where the patch has the same key (B3); entries only in the target are
preserved (B2); patch-only entries are appended (B6). -/
def mergeMap (t p : List (String × Yaml)) : List (String × Yaml) :=
  mergeOld t p ++ onlyNew t p

/-- Target entries, deep-merged with the matching patch entry if any. -/
def mergeOld (t p : List (String × Yaml)) : List (String × Yaml) :=
  match t with
  | [] => []
  | (k, tv) :: rest =>
    match find k p with
    | some pv => (k, merge tv pv) :: mergeOld rest p
    | none => (k, tv) :: mergeOld rest p
end

/-! ### Lookup lemmas -/

/-- `find` over `++` when the key is absent on the left. -/
theorem find_append_right (k : String) (a b : List (String × Yaml))
    (ha : find k a = none) : find k (a ++ b) = find k b := by
  induction a with
  | nil => rfl
  | cons e rest ih =>
    obtain ⟨k', v'⟩ := e
    rw [find] at ha
    split at ha
    · cases ha
    · next hkk => rw [List.cons_append, find, if_neg hkk]; exact ih ha

/-- `find` over `++` when the key is present on the left. -/
theorem find_append_left (k : String) (a b : List (String × Yaml)) (v : Yaml)
    (ha : find k a = some v) : find k (a ++ b) = some v := by
  induction a with
  | nil => cases ha
  | cons e rest ih =>
    obtain ⟨k', v'⟩ := e
    rw [find] at ha
    rw [List.cons_append, find]
    split at ha
    · next hkk => cases ha; rw [if_pos hkk]
    · next hkk => rw [if_neg hkk]; exact ih ha

/-- `mergeOld` keeps exactly the target's keys: a key absent from the patch
keeps its target value. -/
theorem find_mergeOld_of_patch_none (t p : List (String × Yaml)) (k : String)
    (hk : find k p = none) : find k (mergeOld t p) = find k t := by
  induction t with
  | nil => simp [mergeOld]
  | cons e rest ih =>
    obtain ⟨k', tv⟩ := e
    rw [mergeOld]
    by_cases hkk : k' = k
    · subst hkk
      rw [hk]
      simp [find]
    · cases hfp : find k' p with
      | some pv => simp only [find, if_neg hkk]; exact ih
      | none => simp only [find, if_neg hkk]; exact ih

/-- A key absent from the target is absent from `mergeOld`. -/
theorem find_mergeOld_of_target_none (t p : List (String × Yaml)) (k : String)
    (hk : find k t = none) : find k (mergeOld t p) = none := by
  induction t with
  | nil => simp [mergeOld, find]
  | cons e rest ih =>
    obtain ⟨k', tv⟩ := e
    simp only [find] at hk
    split at hk
    · cases hk
    · next hkk =>
      rw [mergeOld]
      cases hfp : find k' p with
      | some pv => simp only [find, if_neg hkk]; exact ih hk
      | none => simp only [find, if_neg hkk]; exact ih hk

/-- A key present in both target and patch maps to the recursive merge
inside `mergeOld`. -/
theorem find_mergeOld_of_both (t p : List (String × Yaml)) (k : String)
    (tv pv : Yaml) (htv : find k t = some tv) (hpv : find k p = some pv) :
    find k (mergeOld t p) = some (merge tv pv) := by
  induction t with
  | nil => cases htv
  | cons e rest ih =>
    obtain ⟨k', tv'⟩ := e
    rw [find] at htv
    rw [mergeOld]
    by_cases hkk : k' = k
    · subst hkk
      rw [if_pos rfl] at htv
      cases htv
      rw [hpv]
      simp [find]
    · rw [if_neg hkk] at htv
      cases hfp : find k' p with
      | some pv' => simp only [find, if_neg hkk]; exact ih htv
      | none => simp only [find, if_neg hkk]; exact ih htv

/-- A key absent from the patch is absent from `onlyNew`. -/
theorem find_onlyNew_of_patch_none (t p : List (String × Yaml)) (k : String)
    (hk : find k p = none) : find k (onlyNew t p) = none := by
  induction p with
  | nil => rfl
  | cons e rest ih =>
    obtain ⟨k', pv'⟩ := e
    simp only [find] at hk
    split at hk
    · cases hk
    · next hkk =>
      rw [onlyNew]
      split
      · simp only [find, if_neg hkk]; exact ih hk
      · exact ih hk

/-- A patch-only key survives the `onlyNew` filter with its patch value. -/
theorem find_onlyNew_of_new (t p : List (String × Yaml)) (k : String) (pv : Yaml)
    (htv : find k t = none) (hpv : find k p = some pv) :
    find k (onlyNew t p) = some pv := by
  induction p with
  | nil => cases hpv
  | cons e rest ih =>
    obtain ⟨k', pv'⟩ := e
    simp only [find] at hpv
    rw [onlyNew]
    by_cases hkk : k' = k
    · subst hkk
      rw [if_pos rfl] at hpv
      cases hpv
      simp [htv, find]
    · rw [if_neg hkk] at hpv
      split
      · simp only [find, if_neg hkk]; exact ih hpv
      · exact ih hpv

/-! ### The spec's merge behaviors as theorems -/

/-- **B2**: a key absent from the patch keeps its target value untouched. -/
theorem merge_preserves_absent (t p : List (String × Yaml)) (k : String)
    (hk : find k p = none) :
    find k (mergeMap t p) = find k t := by
  rw [mergeMap]
  cases htv : find k t with
  | some tv =>
    have h1 : find k (mergeOld t p) = some tv := by
      rw [find_mergeOld_of_patch_none t p k hk, htv]
    rw [find_append_left k _ _ tv h1]
  | none =>
    rw [find_append_right k _ _ (find_mergeOld_of_target_none t p k htv)]
    rw [find_onlyNew_of_patch_none t p k hk]

/-- **B1/B3**: a key present in both maps to the recursive merge of the two
values (scalar overwrite and nested deep-merge are both instances). -/
theorem merge_merges_present (t p : List (String × Yaml)) (k : String)
    (tv pv : Yaml) (htv : find k t = some tv) (hpv : find k p = some pv) :
    find k (mergeMap t p) = some (merge tv pv) := by
  rw [mergeMap]
  exact find_append_left k _ _ _ (find_mergeOld_of_both t p k tv pv htv hpv)

/-- **B6**: a key only in the patch is added with the patch's value. -/
theorem merge_adds_new (t p : List (String × Yaml)) (k : String) (pv : Yaml)
    (htv : find k t = none) (hpv : find k p = some pv) :
    find k (mergeMap t p) = some pv := by
  rw [mergeMap]
  rw [find_append_right k _ _ (find_mergeOld_of_target_none t p k htv)]
  exact find_onlyNew_of_new t p k pv htv hpv

end LoomSpec
