// Proving fixture for #3100: Bean Validation extractor enhancements.
//
// Demonstrates:
//   1. Custom ConstraintValidator<A,T> implementations (custom_validator_extraction)
//   2. @Constraint meta-annotation on custom constraint annotations
//   3. Constraint bounds (min/max/pattern) on @Size/@Min/@Max/@Pattern (constraint_extraction)
//   4. Nested @Valid field-level recursion into DTO types (nested_model_extraction)
//   5. Field-level @NotNull/@Size etc. on DTO classes (schema_extraction)
package fixtures.java.bean_validation;

import jakarta.validation.constraints.*;
import jakarta.validation.Valid;
import jakarta.validation.Validated;
import jakarta.validation.Constraint;
import jakarta.validation.ConstraintValidator;
import jakarta.validation.ConstraintValidatorContext;
import jakarta.validation.Payload;
import java.lang.annotation.*;

// ─────────────────────────────────────────────────────────────────────────────
// 1. Custom @Constraint annotation
// ─────────────────────────────────────────────────────────────────────────────

@Documented
@Constraint(validatedBy = PhoneNumberValidator.class)
@Target({ElementType.FIELD, ElementType.PARAMETER})
@Retention(RetentionPolicy.RUNTIME)
public @interface ValidPhoneNumber {
    String message() default "Invalid phone number";
    Class<?>[] groups() default {};
    Class<? extends Payload>[] payload() default {};
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. ConstraintValidator<A,T> implementation — the core custom_validator_extraction target
// ─────────────────────────────────────────────────────────────────────────────

public class PhoneNumberValidator implements ConstraintValidator<ValidPhoneNumber, String> {
    @Override
    public void initialize(ValidPhoneNumber annotation) {}

    @Override
    public boolean isValid(String value, ConstraintValidatorContext context) {
        if (value == null) return true;
        return value.matches("\\+?[0-9]{7,15}");
    }
}

// Another custom validator (tests multi-validator detection)
public class PositiveAmountValidator implements ConstraintValidator<PositiveAmount, java.math.BigDecimal> {
    @Override
    public boolean isValid(java.math.BigDecimal value, ConstraintValidatorContext ctx) {
        return value != null && value.compareTo(java.math.BigDecimal.ZERO) > 0;
    }
}

@Documented
@Constraint(validatedBy = PositiveAmountValidator.class)
@Target({ElementType.FIELD})
@Retention(RetentionPolicy.RUNTIME)
public @interface PositiveAmount {
    String message() default "Amount must be positive";
    Class<?>[] groups() default {};
    Class<? extends Payload>[] payload() default {};
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. DTO with field-level constraints + bounds (schema_extraction + constraint_extraction)
// ─────────────────────────────────────────────────────────────────────────────

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

    @Pattern(regexp = "^[A-Z]{2}$", message = "Must be 2-letter country code")
    private String countryCode;

    @ValidPhoneNumber
    private String contactPhone;

    @PositiveAmount
    private java.math.BigDecimal price;

    // 4. @Valid recursion into nested DTO (nested_model_extraction)
    @NotNull
    @Valid
    private ShippingAddress shippingAddress;

    // Double-nested: @Valid on a list element type forces recursion
    @Valid
    private java.util.List<OrderItem> items;
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Nested DTO classes (nested_model_extraction targets)
// ─────────────────────────────────────────────────────────────────────────────

public class ShippingAddress {
    @NotBlank
    private String street;

    @NotBlank
    @Size(min = 2, max = 100)
    private String city;

    @NotBlank
    @Pattern(regexp = "^[A-Z]{2}$")
    private String countryCode;

    @Size(min = 5, max = 10)
    private String postalCode;
}

public class OrderItem {
    @NotNull
    private String sku;

    @Min(1)
    @Max(999)
    private int quantity;

    @PositiveAmount
    private java.math.BigDecimal unitPrice;
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. @Validated on a service class (shows class-level constraint recognition)
// ─────────────────────────────────────────────────────────────────────────────

@Validated
public class OrderService {
    public void placeOrder(@Valid @NotNull CreateOrderRequest request) {}

    public OrderItem getItem(@Min(1) Long itemId) {}
}
