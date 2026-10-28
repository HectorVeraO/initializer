import { DOCUMENT, Location } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { AbstractControl, FormControl, FormGroup, ReactiveFormsModule, ValidationErrors, Validators } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { TranslocoModule, TranslocoService, provideTranslocoScope } from '@jsverse/transloco';
import { MessageService } from '@openng/optimus-ui/api';
import { ButtonModule } from '@openng/optimus-ui/button';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { MessageModule } from '@openng/optimus-ui/message';
import { firstValueFrom } from 'rxjs';
import { ApiErrorCode } from '@/core/api/api-error-codes';
import { errorMessage } from '@/core/api/api-error';
import { SubmitErrors } from '@/core/api/submit-errors';
import { PLATFORM_CORE_I18N } from '../module';
import { SessionStore } from '../session/session-store.service';
import { fieldIssueMessages, issueMessage, unmatchedIssueMessages } from '../ui/issue-message';
import { InvitationsApi } from './invitations-api.service';
import { ACCEPT_PATH, tokenFromHash } from './invitation-token';
import { PendingInvitation } from './pending-invitation.service';

const SCOPE_ALIAS = PLATFORM_CORE_I18N.alias as string;

const NEW_ACCOUNT_FIELDS: readonly string[] = ['fullName', 'password'];

function passwordsMatch(group: AbstractControl): ValidationErrors | null {
  const password = group.get('password')?.value as string;

  const confirm = group.get('confirm')?.value as string;

  return password === confirm ? null : { mismatch: true };
}

/** Public page of an invitation link: accepts it as the signed-in account, or creates the account. The token never leaves this component's signal. */
@Component({
  selector: 'app-accept-invitation-page',
  imports: [ReactiveFormsModule, RouterLink, ButtonModule, InputTextModule, MessageModule, TranslocoModule],
  providers: [provideTranslocoScope(PLATFORM_CORE_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './accept-invitation-page.component.html'
})
export class AcceptInvitationPage {
  private readonly api = inject(InvitationsApi);
  private readonly store = inject(SessionStore);
  private readonly router = inject(Router);
  private readonly transloco = inject(TranslocoService);
  private readonly messages = inject(MessageService);
  private readonly pending = inject(PendingInvitation);
  private readonly location = inject(Location);

  private readonly token = signal<string | null>(null);
  private acceptedTenantId: string | null = null;

  protected readonly isLoaded = signal(false);
  protected readonly isSubmitting = signal(false);
  protected readonly isInvalidLink = signal(false);
  protected readonly isAlreadyMember = signal(false);
  protected readonly highlightSignIn = signal(false);
  protected readonly errorText = signal<string | null>(null);
  protected readonly submitErrors = new SubmitErrors();
  protected readonly showMismatch = signal(false);

  protected readonly hasToken = computed(() => this.token() !== null);
  protected readonly isAuthenticated = computed(() => this.store.state() === 'authenticated');
  protected readonly accountEmail = computed(() => this.store.account()?.email ?? '');

  protected readonly form = new FormGroup(
    {
      fullName: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
      password: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
      confirm: new FormControl('', { nonNullable: true, validators: [Validators.required] })
    },
    { validators: [passwordsMatch] }
  );

  protected readonly fullNameMessages = computed(() => fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), 'fullName'));
  protected readonly passwordMessages = computed(() => fieldIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), 'password'));
  protected readonly formMessages = computed(() => unmatchedIssueMessages(this.transloco, SCOPE_ALIAS, this.submitErrors.issues(), [...NEW_ACCOUNT_FIELDS, 'token']));

  constructor() {
    const document = inject(DOCUMENT);

    const fromHash = tokenFromHash(document.location.hash);

    // The fragment leaves the address bar and the history entry as soon as it is read.
    if (document.location.hash !== '') {
      this.location.replaceState(ACCEPT_PATH, '', this.location.getState());
    }

    this.token.set(fromHash ?? this.pending.take());

    // The router's own URL keeps the fragment until it navigates; later navigations copy router.url.
    if (document.location.hash !== '') {
      void this.router.navigateByUrl(ACCEPT_PATH, { replaceUrl: true });
    }

    void this.start();
  }

  protected async submitAccept(): Promise<void> {
    const token = this.token();

    if (token === null || this.isSubmitting()) {
      return;
    }

    this.isSubmitting.set(true);
    this.errorText.set(null);
    this.isAlreadyMember.set(false);

    try {
      // A retry after a failed switch must not resend the accepted token: the server answers 404 for it.
      if (this.acceptedTenantId === null) {
        const result = await firstValueFrom(this.api.accept(token));

        this.acceptedTenantId = result.membership.tenant.id;
      }

      await this.switchToInvitedTenant(this.acceptedTenantId);
    } catch (error) {
      this.showAcceptError(error);
    } finally {
      this.isSubmitting.set(false);
    }
  }

  /** The invitation is already accepted here: a failed switch is reported as itself, never as an invalid invitation. */
  private async switchToInvitedTenant(tenantId: string): Promise<void> {
    try {
      await this.store.switchTenant(tenantId);
    } catch (error) {
      this.errorText.set(errorMessage(this.transloco, error));

      return;
    }

    this.messages.add({ severity: 'success', summary: this.transloco.translate('core.accept.accepted') });
    await this.router.navigateByUrl('/home');
  }

  protected signIn(): void {
    const token = this.token();

    if (token === null) {
      return;
    }

    this.pending.set(token);
    void this.router.navigate(['/login'], { queryParams: { returnUrl: ACCEPT_PATH } });
  }

  protected async submitNew(): Promise<void> {
    const token = this.token();

    this.showMismatch.set(false);
    this.form.markAllAsTouched();

    if (token === null || this.isSubmitting() || this.form.controls.fullName.invalid || this.form.controls.password.invalid || this.form.controls.confirm.invalid) {
      return;
    }

    if (this.form.hasError('mismatch')) {
      this.showMismatch.set(true);

      return;
    }

    this.isSubmitting.set(true);
    this.errorText.set(null);
    this.highlightSignIn.set(false);
    this.submitErrors.clear();

    try {
      const result = await firstValueFrom(this.api.acceptNew({ token, password: this.form.controls.password.value, fullName: this.form.controls.fullName.value.trim() }));

      await this.router.navigate(['/login'], { state: { email: result.account.email, noticeKey: 'accountCreated' } });
    } catch (error) {
      this.showNewError(error);
    } finally {
      this.isSubmitting.set(false);
    }
  }

  private async start(): Promise<void> {
    try {
      await this.store.ensureLoaded();
    } catch {
      this.errorText.set(this.transloco.translate('errors.codes.SERVICE_UNAVAILABLE'));
    }

    this.isLoaded.set(true);
  }

  private showAcceptError(error: unknown): void {
    const failure = new SubmitErrors();

    failure.record(error);

    const status = failure.status();

    const code = failure.code();

    if (status === 404 || status === 422) {
      this.errorText.set(this.transloco.translate('errors.codes.INVITATION_INVALID'));
    } else if (code === ApiErrorCode.InvitationEmailMismatch) {
      this.errorText.set(this.transloco.translate('core.accept.emailMismatch'));
    } else if (code === ApiErrorCode.MembershipAlreadyActive) {
      this.isAlreadyMember.set(true);
    } else {
      this.errorText.set(errorMessage(this.transloco, error));
    }
  }

  private showNewError(error: unknown): void {
    this.submitErrors.record(error);

    const status = this.submitErrors.status();

    if (status === 409) {
      this.highlightSignIn.set(true);
      this.errorText.set(this.transloco.translate('core.accept.accountExists'));
    } else if (status === 422) {
      const hasTokenIssue = this.submitErrors.issues().some((issue) => issue.field === 'token');

      this.isInvalidLink.set(hasTokenIssue);
    } else if (status === 404) {
      this.errorText.set(this.transloco.translate(`errors.codes.${ApiErrorCode.InvitationInvalid}`));
    } else {
      this.errorText.set(errorMessage(this.transloco, error));
    }
  }

  /** Text for one issue code, for specs and the template alike. */
  protected issueText(code: string): string {
    return issueMessage(this.transloco, SCOPE_ALIAS, code);
  }
}
