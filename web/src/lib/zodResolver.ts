import type { FieldError, FieldErrors, FieldValues, Resolver } from "react-hook-form";
import type { z } from "zod";

/** Validates a react-hook-form form with a zod schema, in place of @hookform/resolvers (whose dependencies include validators Encore doesn't use). Valid values come back parsed, so transforms such as trim apply; each issue becomes its field's error, the first one winning, at the field's path. */
export function zodResolver<T extends FieldValues>(schema: z.ZodType<T>): Resolver<T> {
  return async (values) => {
    const result = await schema.safeParseAsync(values);
    if (result.success) return { errors: {}, values: result.data };

    const errors: Record<string, unknown> = {};
    for (const issue of result.error.issues) {
      const path = issue.path.map(String);
      const field = path.pop();
      if (field === undefined) continue;
      let node = errors;
      for (const key of path) {
        node[key] ??= {};
        node = node[key] as Record<string, unknown>;
      }
      node[field] ??= { message: issue.message, type: issue.code } satisfies FieldError;
    }
    return { errors: errors as FieldErrors<T>, values: {} };
  };
}
