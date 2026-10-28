import { ChangeDetectionStrategy, Component, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { FormControl, FormGroup, ReactiveFormsModule } from '@angular/forms';
import { TranslocoModule, TranslocoService, provideTranslocoScope } from '@jsverse/transloco';
import { ButtonModule } from '@openng/optimus-ui/button';
import { DialogModule } from '@openng/optimus-ui/dialog';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { MessageModule } from '@openng/optimus-ui/message';
import { SelectModule } from '@openng/optimus-ui/select';
import { ToggleSwitchModule } from '@openng/optimus-ui/toggleswitch';
import { firstValueFrom, startWith } from 'rxjs';
import { errorMessage } from '@/core/api/api-error';
import { SubmitErrors } from '@/core/api/submit-errors';
import { fieldIssueMessages, unmatchedIssueMessages } from '@/platform/core';
import { jsonText, parseJsonText } from '../json-value';
import { PLATFORM_PARAMETERS_I18N } from '../i18n/scope';
import { ParametersApi } from '../parameters-api.service';
import type { ParameterReplace, ParameterValueType, ParameterView } from '../parameters.types';

const SCOPE_ALIAS = PLATFORM_PARAMETERS_I18N.alias as string;

const FORM_FIELDS: readonly string[] = ['key', 'valueType', 'defaultValue', 'minValue', 'maxValue', 'allowedValues', 'description', 'isSystemRequired'];

const ALREADY_EXISTS = 'PARAMETER_ALREADY_EXISTS';

/** Types a definition can have, in the order the type select lists them. */
export const PARAMETER_VALUE_TYPES: ParameterValueType[] = ['string', 'number', 'boolean', 'json'];

type FieldName = 'defaultValue' | 'minValue' | 'maxValue' | 'allowedValues';

type FieldRead<T> = { readonly ok: true; readonly value: T } | { readonly ok: false };

/** Empty text is "no bound" (null); anything else must be a finite number. */
function readBound(text: string): FieldRead<number | null> {
  const trimmed = text.trim();

  if (trimmed === '') {
    return { ok: true, value: null };
  }

  const value = Number(trimmed);

  const isFinite = Number.isFinite(value);

  return isFinite ? { ok: true, value } : { ok: false };
}

/** A numeric input yields null when empty; anything else must be finite. */
function readNumber(value: number | null): FieldRead<number | null> {
  const isValid = value === null || Number.isFinite(value);

  return isValid ? { ok: true, value } : { ok: false };
}

/** Empty text is "no list" (null); anything else must be a JSON array. */
function readAllowed(text: string): FieldRead<unknown[] | null> {
  if (text.trim() === '') {
    return { ok: true, value: null };
  }

  const parsed = parseJsonText(text);

  if (parsed.ok && Array.isArray(parsed.value)) {
    return { ok: true, value: parsed.value };
  }

  return { ok: false };
}

/** Creates a parameter definition, or edits one when `parameter` is set. `key`, `valueType` and `isSystemRequired` are read-only on edit. */
@Component({
  selector: 'app-parameter-definition-dialog',
  imports: [ReactiveFormsModule, ButtonModule, DialogModule, InputTextModule, MessageModule, SelectModule, ToggleSwitchModule, TranslocoModule],
  providers: [provideTranslocoScope(PLATFORM_PARAMETERS_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './parameter-definition-dialog.component.html'
})
export class ParameterDefinitionDialog {
  private readonly api = inject(ParametersApi);
  private readonly transloco = inject(TranslocoService);

  readonly visible = input.required<boolean>();
  /** The definition to edit; null creates a new one. */
  readonly parameter = input<ParameterView | null>(null);
  readonly saved = output<ParameterView>();
  readonly closed = output<void>();

  protected readonly valueTypes = PARAMETER_VALUE_TYPES;

  protected readonly form = new FormGroup({
    key: new FormControl('', { nonNullable: true }),
    valueType: new FormControl<ParameterValueType>('string', { nonNullable: true }),
    defaultText: new FormControl('', { nonNullable: true }),
    defaultBoolean: new FormControl(false, { nonNullable: true }),
    min: new FormControl<number | null>(null),
    max: new FormControl<number | null>(null),
    allowedText: new FormControl('', { nonNullable: true }),
    description: new FormControl('', { nonNullable: true }),
    isSystemRequired: new FormControl(false, { nonNullable: true })
  });
  protected readonly valueType = toSignal(this.form.controls.valueType.valueChanges.pipe(startWith(this.form.controls.valueType.value)), { requireSync: true });

  protected readonly isEdit = computed(() => this.parameter() !== null);
  protected readonly isSubmitting = signal(false);
  protected readonly submitErrors = new SubmitErrors();
  private readonly lastError = signal<unknown>(null);
  /** Fields whose text did not parse on the last save; nothing was sent. */
  protected readonly localErrors = signal<Partial<Record<FieldName, string>>>({});

  protected readonly keyMessages = computed(() => {
    const messages = fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), 'key');

    if (this.submitErrors.conflictWith(ALREADY_EXISTS)) {
      messages.push(this.transloco.translate(`${SCOPE_ALIAS}.errors.${ALREADY_EXISTS}`));
    }

    return messages;
  });
  protected readonly typeMessages = computed(() => fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), 'valueType'));
  protected readonly defaultMessages = computed(() => this.withLocal('defaultValue'));
  protected readonly minMessages = computed(() => this.withLocal('minValue'));
  protected readonly maxMessages = computed(() => this.withLocal('maxValue'));
  protected readonly allowedMessages = computed(() => this.withLocal('allowedValues'));
  protected readonly descriptionMessages = computed(() => fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), 'description'));
  protected readonly systemMessages = computed(() => fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), 'isSystemRequired'));

  protected readonly formMessages = computed(() => {
    const messages = unmatchedIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), FORM_FIELDS);

    const status = this.submitErrors.status();

    const isHandledElsewhere = status === null || status === 422 || this.submitErrors.conflictWith(ALREADY_EXISTS);

    if (!isHandledElsewhere) {
      messages.push(this.failureMessage());
    }

    return messages;
  });

  constructor() {
    // Each time the dialog opens, or points at another definition, the form starts from it.
    effect(() => {
      const isVisible = this.visible();

      const parameter = this.parameter();

      untracked(() => {
        if (isVisible) {
          this.reset(parameter);
        }
      });
    });
  }

  protected async save(): Promise<void> {
    if (this.isSubmitting()) {
      return;
    }

    const input = this.readForm();

    if (input === null) {
      return;
    }

    this.isSubmitting.set(true);
    this.submitErrors.clear();
    this.lastError.set(null);

    try {
      const existing = this.parameter();

      const saved = await firstValueFrom(existing === null ? this.api.create({ key: this.form.controls.key.value, ...input }) : this.api.update(existing.key, input));

      this.saved.emit(saved);
      this.closed.emit();
    } catch (error) {
      this.lastError.set(error);
      this.submitErrors.record(error);
    } finally {
      this.isSubmitting.set(false);
    }
  }

  protected close(): void {
    this.closed.emit();
  }

  protected onVisibleChange(isVisible: boolean): void {
    if (!isVisible) {
      this.closed.emit();
    }
  }

  /** The mutable fields of the request body, or null (after marking the fields) when a text does not parse. */
  private readForm(): ParameterReplace | null {
    const errors: Partial<Record<FieldName, string>> = {};

    const defaultValue = this.readDefault();

    const min = readNumber(this.form.controls.min.value);

    const max = readNumber(this.form.controls.max.value);

    const allowed = readAllowed(this.form.controls.allowedText.value);

    if (!defaultValue.ok) {
      errors.defaultValue = this.valueType() === 'number' ? 'numberInvalid' : this.valueType() === 'json' ? 'jsonInvalid' : 'booleanInvalid';
    }

    if (!min.ok) {
      errors.minValue = 'numberInvalid';
    }

    if (!max.ok) {
      errors.maxValue = 'numberInvalid';
    }

    if (!allowed.ok) {
      errors.allowedValues = 'allowedInvalid';
    }

    this.localErrors.set(errors);

    if (!defaultValue.ok || !min.ok || !max.ok || !allowed.ok) {
      return null;
    }

    const body: ParameterReplace = {
      valueType: this.valueType(),
      defaultValue: defaultValue.value,
      minValue: min.value,
      maxValue: max.value,
      allowedValues: allowed.value,
      description: this.form.controls.description.value,
      isSystemRequired: this.form.controls.isSystemRequired.value
    };

    return body;
  }

  private readDefault(): FieldRead<unknown> {
    const type = this.valueType();

    if (type === 'string') {
      return { ok: true, value: this.form.controls.defaultText.value };
    }

    if (type === 'boolean') {
      return { ok: true, value: this.form.controls.defaultBoolean.value };
    }

    if (type === 'json') {
      const parsed = parseJsonText(this.form.controls.defaultText.value);

      return parsed;
    }

    const bound = readBound(this.form.controls.defaultText.value);

    const isNumber = bound.ok && bound.value !== null;

    return isNumber ? bound : { ok: false };
  }

  private withLocal(field: FieldName): string[] {
    const messages = fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), field);

    const local = this.localErrors()[field];

    if (local !== undefined) {
      messages.push(this.transloco.translate(`${SCOPE_ALIAS}.definition.${local}`));
    }

    return messages;
  }

  /** A known conflict code has its own text in this scope; anything else falls back to the generic mapping. */
  private failureMessage(): string {
    const code = this.submitErrors.code();

    const key = `${SCOPE_ALIAS}.errors.${code}`;

    const translated = code === null ? key : this.transloco.translate(key);

    const isKnown = code !== null && translated !== key;

    return isKnown ? translated : errorMessage(this.transloco, this.lastError());
  }

  private reset(parameter: ParameterView | null): void {
    this.submitErrors.clear();
    this.lastError.set(null);
    this.localErrors.set({});
    this.isSubmitting.set(false);
    const current = parameter?.defaultValue;

    const defaultText = parameter === null ? '' : parameter.valueType === 'string' ? String(current ?? '') : parameter.valueType === 'boolean' ? '' : jsonText(current);

    const isEditing = parameter !== null;

    this.form.reset({
      key: parameter?.key ?? '',
      valueType: parameter?.valueType ?? 'string',
      defaultText,
      defaultBoolean: parameter?.defaultValue === true,
      min: parameter?.minValue ?? null,
      max: parameter?.maxValue ?? null,
      allowedText: parameter?.allowedValues == null ? '' : jsonText(parameter.allowedValues),
      description: parameter?.description ?? '',
      isSystemRequired: parameter?.isSystemRequired ?? false
    });

    // Type and system flag are fixed once the definition exists.
    const fixedControls = [this.form.controls.valueType, this.form.controls.isSystemRequired];

    for (const control of fixedControls) {
      if (isEditing) {
        control.disable();
      } else {
        control.enable();
      }
    }
  }
}
