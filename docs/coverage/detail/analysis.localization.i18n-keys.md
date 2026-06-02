<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `analysis.localization.i18n-keys` — i18n translation-key usage (USES_TRANSLATION)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** App Topology & Integration
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Translation key usage | 🟢 `partial` | `2026-06-02` | 3628 | `internal/extractor/translation_key.go`<br>`internal/extractor/translation_key_test.go`<br>`internal/extractors/javascript/translation_key.go`<br>`internal/extractors/javascript/translation_key_test.go`<br>`internal/extractors/php/translation_key.go`<br>`internal/extractors/php/translation_key_test.go`<br>`internal/extractors/python/translation_key.go`<br>`internal/extractors/python/translation_key_test.go`<br>`internal/extractors/ruby/translation_key.go`<br>`internal/extractors/ruby/translation_key_test.go`<br>`internal/types/kinds.go` | #3628 area: i18n / localization KEY-USAGE detection. Each language extractor emits a USES_TRANSLATION edge from the enclosing function/component to a synthetic SCOPE.TranslationKey node (Name "i18n:<key>") so every reference site of a literal key converges on ONE node and the graph answers "where is the 'errors.notFound' string used?" and supports untranslated-key analysis (a key node with no backing catalog entry). Shared node/edge builders live in internal/extractor/translation_key.go (TranslationKeyEntity, TranslationKeyTargetID, EmitTranslationKeyEdges) with the JS/TS i18n import dictionary (IsI18nImportSource) and the static-key guard (IsStaticTranslationKey). JS/TS pass: react-i18next/i18next t('k')/i18n.t('k')/<Trans i18nKey> and vue-i18n $t('k') (bare t requires an i18n import in the file). Python pass: Django/gettext _('m')/gettext('x')/gettext_lazy('x') import-gated to django.utils.translation / gettext. Ruby pass: Rails I18n.t('k') and relative t('.k'). PHP pass: Laravel __('k')/trans('k'). PRECISION-FIRST / REQUIRE-I18N-CONTEXT honest-partial: a dynamic key (t(keyVar), interpolated) or a non-i18n _('x') (lodash) / unrelated t(...) without i18n context emits NO node/edge; an ambiguous bare Rails t('plain') (no receiver, no leading dot) is dropped. PARTIAL because jsts/python/ruby/php lanes are implemented (java/go and the Blade @lang directive in *.blade.php template text are future lanes) and recall is deliberately bounded to static literal keys. DEPLOY-DEFERRED: extractor + kinds land here; live-daemon reindex is a separate coordinated step. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update analysis.localization.i18n-keys ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
