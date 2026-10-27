import { ChangeDetectionStrategy, Component, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { FormControl, ReactiveFormsModule } from '@angular/forms';
import { TranslocoModule, TranslocoService, provideTranslocoScope } from '@jsverse/transloco';
import { ButtonModule } from '@openng/optimus-ui/button';
import { DialogModule } from '@openng/optimus-ui/dialog';
import { InputNumberModule } from '@openng/optimus-ui/inputnumber';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { MessageModule } from '@openng/optimus-ui/message';
import { SelectModule } from '@openng/optimus-ui/select';
import { TextareaModule } from '@openng/optimus-ui/textarea';
import { ToggleSwitchModule } from '@openng/optimus-ui/toggleswitch';
import { firstValueFrom } from 'rxjs';
import { errorMessage } from '@/core/api/api-error';
import { SubmitErrors } from '@/core/api/submit-errors';
import { fieldIssueMessages, unmatchedIssueMessages } from '@/gatbase/core';
import { jsonText, parseJsonText } from '../json-value';
import { GATBASE_PARAMETERS_I18N } from '../module';
import { ParametersApi } from '../parameters-api.service';
import type { ParameterView } from '../parameters.types';

const SCOPE_ALIAS = GATBASE_PARAMETERS_I18N.alias as string;

const FORM_FIELDS: readonly string[] = ['value'];

type EditorKind = 'select' | 'number' | 'boolean' | 'string' | 'json';

/** Sets the tenant override of one parameter. Shown while `parameter` is not null; the server decides whether the value is valid. */
@Component({
  selector: 'app-parameter-override-dialog',
  imports: [ReactiveFormsModule, ButtonModule, DialogModule, InputNumberModule, InputTextModule, MessageModule, SelectModule, TextareaModule, ToggleSwitchModule, TranslocoModule],
  providers: [provideTranslocoScope(GATBASE_PARAMETERS_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './parameter-override-dialog.component.html'
})
export class ParameterOverrideDialog {
  private readonly api = inject(ParametersApi);
  private readonly transloco = inject(TranslocoService);

  /** The parameter to override; null hides the dialog. */
  readonly parameter = input.required<ParameterView | null>();
  /** Emits the parameter as the server answered after a saved override. */
  readonly saved = output<ParameterView>();
  readonly closed = output<void>();

  /** The editor value: a choice, a number, a boolean or the text, by `kind`. */
  protected readonly value = new FormControl<unknown>(null);
  protected readonly hasJsonError = signal(false);
  protected readonly isSubmitting = signal(false);
  protected readonly submitErrors = new SubmitErrors();
  private readonly lastError = signal<unknown>(null);

  protected readonly kind = computed<EditorKind>(() => {
    const parameter = this.parameter();

    const hasAllowedValues = parameter !== null && parameter.allowedValues !== null && parameter.allowedValues.length > 0;

    if (hasAllowedValues) {
      return 'select';
    }

    const valueType = parameter?.valueType ?? 'string';

    return valueType;
  });

  protected readonly valueMessages = computed(() => fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), 'value'));

  protected readonly formMessages = computed(() => {
    const messages = unmatchedIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), FORM_FIELDS);

    const status = this.submitErrors.status();

    const isHandledElsewhere = status === null || status === 422;

    if (!isHandledElsewhere) {
      messages.push(errorMessage(this.transloco, this.lastError()));
    }

    return messages;
  });

  constructor() {
    // Each time the dialog opens it starts from the value in effect.
    effect(() => {
      const parameter = this.parameter();

      untracked(() => this.reset(parameter));
    });
  }

  protected onJsonInput(): void {
    this.hasJsonError.set(false);
  }

  protected async save(): Promise<void> {
    const parameter = this.parameter();

    if (parameter === null || this.isSubmitting()) {
      return;
    }

    const read = this.readValue();

    if (!read.ok) {
      this.hasJsonError.set(true);

      return;
    }

    this.isSubmitting.set(true);
    this.submitErrors.clear();
    this.lastError.set(null);

    try {
      const updated = await firstValueFrom(this.api.setOverride(parameter.key, read.value));

      this.saved.emit(updated);
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

  /** The value the editor holds, or not ok when a JSON text does not parse. */
  private readValue(): { readonly ok: true; readonly value: unknown } | { readonly ok: false } {
    const kind = this.kind();

    if (kind === 'json') {
      const parsed = parseJsonText(String(this.value.value ?? ''));

      return parsed;
    }

    return { ok: true, value: this.value.value };
  }

  private reset(parameter: ParameterView | null): void {
    this.submitErrors.clear();
    this.lastError.set(null);
    this.hasJsonError.set(false);
    this.isSubmitting.set(false);

    if (parameter === null) {
      return;
    }

    const current = parameter.effectiveValue;

    const valueByKind: Record<EditorKind, unknown> = {
      select: current,
      number: typeof current === 'number' ? current : null,
      boolean: current === true,
      string: typeof current === 'string' ? current : '',
      json: jsonText(current)
    };

    this.value.setValue(valueByKind[this.kind()]);
  }
}
