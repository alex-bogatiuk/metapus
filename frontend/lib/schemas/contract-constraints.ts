import { z } from "zod"

// Validate the wire contract first. Intersections can discard unrecognized-key
// errors reported by only one side, so they must not enforce request strictness.
// Constraints refine values without replacing the generated transport projection.
export function constrainContract<T, R>(contract: z.ZodType<T>, constraints: z.ZodType<R>): z.ZodType<T> {
  return contract.superRefine((value, context) => {
    const result = constraints.safeParse(value)
    if (!result.success) {
      for (const issue of result.error.issues) context.addIssue({ ...issue })
    }
  })
}
