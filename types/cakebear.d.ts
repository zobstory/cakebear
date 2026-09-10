// cakebear's type extensions.
//
// These are branded types: an intersection of the underlying primitive with a
// phantom property that no value can actually have. TypeScript's own checker
// enforces them, which is the entire point -- cakebear adds types without a
// single edit inside internal/checker, so upstream merges stay mechanical.
//
// Each brand comes with a conversion function of the same name. A type and a
// value may share a name in TypeScript, so `i32` is both the type and the way
// to produce one:
//
//     const n: i32 = i32(5);
//
// The conversion is deliberately explicit. `i32 + i32` is typed `number` by
// TypeScript, because adding two 32-bit integers can overflow into something
// that is not one, so widening back requires `i32(a + b)` and the truncation is
// visible in the source rather than implied.
//
// Per cakebear's own principle, any of these is retired the moment TypeScript
// ships an equivalent.

declare type i32 = number & { readonly __cakebearBrand: "i32" };
declare type i64 = number & { readonly __cakebearBrand: "i64" };
declare type u32 = number & { readonly __cakebearBrand: "u32" };
declare type u64 = number & { readonly __cakebearBrand: "u64" };
declare type f32 = number & { readonly __cakebearBrand: "f32" };

/** A 32-bit signed integer. Lowers to Go's int32. */
declare function i32(value: number): i32;
/** A 64-bit signed integer. Lowers to Go's int64. */
declare function i64(value: number): i64;
/** A 32-bit unsigned integer. Lowers to Go's uint32. */
declare function u32(value: number): u32;
/** A 64-bit unsigned integer. Lowers to Go's uint64. */
declare function u64(value: number): u64;
/** A 32-bit float. Lowers to Go's float32. */
declare function f32(value: number): f32;

declare type base64 = string & { readonly __cakebearBrand: "base64" };

/**
 * A base64-encoded string.
 *
 * This is a compile-time refinement over `string`, not a distinct runtime
 * representation: base64 *is* a string, and giving it its own runtime type
 * would force conversions at every boundary while buying nothing. What the
 * type buys is that an arbitrary string cannot be passed where base64 is
 * expected.
 *
 * A string literal argument is validated when the program is compiled, so
 * `base64("!!!")` is a compile error rather than a runtime surprise.
 */
declare function base64(value: string): base64;
