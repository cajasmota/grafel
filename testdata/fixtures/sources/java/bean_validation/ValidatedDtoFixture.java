// Proving fixture for #3002: Bean Validation schema_extraction + tests_linkage.
//
// Demonstrates parameter-level Bean Validation constraint annotations
// (@NotNull, @Size, @Min, @Max, @Email, @NotBlank) as extracted by
// internal/engine/java_annotation_params.go.
//
// Note: field-level recursion into nested model classes is NOT extracted
// (schema_extraction is partial, not full). Parameter-level constraints
// are fully captured.
package fixtures.java.bean_validation;

import jakarta.validation.constraints.*;
import jakarta.validation.Valid;

/**
 * DTO class showing field-level Bean Validation annotations.
 * These field-level annotations are NOT recursively extracted by
 * the current extractor (schema_extraction=partial scope).
 */
public class CreateOrderRequest {
    @NotNull
    @Size(min = 1, max = 255)
    private String productId;

    @Min(1)
    @Max(1000)
    private int quantity;

    @Email
    @NotBlank
    private String customerEmail;

    @NotNull
    @Valid
    private ShippingAddress shippingAddress;
}

public class ShippingAddress {
    @NotBlank
    private String street;

    @NotBlank
    @Size(min = 2, max = 2)
    private String countryCode;
}
