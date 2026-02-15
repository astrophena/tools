import { useCallback, useState } from "npm:react";

import { errorMessage } from "../api.ts";
import { EditableResource } from "../types.ts";

/** Hook configuration for load/save handlers. */
type UseEditableResourceOptions = {
  load: () => Promise<string>;
  save: (value: string) => Promise<void>;
};

/**
 * Manages editable text resources with baseline tracking and save/load state.
 */
export function useEditableResource(options: UseEditableResourceOptions): EditableResource {
  const [value, setValue] = useState("");
  const [baseline, setBaseline] = useState("");
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const next = await options.load();
      setValue(next);
      setBaseline(next);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  }, [options]);

  const save = useCallback(async (): Promise<boolean> => {
    setSaving(true);
    setError("");
    try {
      await options.save(value);
      setBaseline(value);
      return true;
    } catch (err) {
      setError(errorMessage(err));
      return false;
    } finally {
      setSaving(false);
    }
  }, [options, value]);

  return {
    value,
    setValue,
    dirty: value !== baseline,
    loading,
    saving,
    error,
    load,
    save,
  };
}
