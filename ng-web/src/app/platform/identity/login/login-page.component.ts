import { DOCUMENT } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormControl, FormGroup, ReactiveFormsModule } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { TranslocoModule, TranslocoService, provideTranslocoScope } from '@jsverse/transloco';
import { ButtonModule } from '@openng/optimus-ui/button';
import { InputTextModule } from '@openng/optimus-ui/inputtext';
import { MessageModule } from '@openng/optimus-ui/message';
import { KratosLoginClient } from '../auth/kratos-login.service';
import type { KratosMessage } from '../auth/kratos.types';
import { scopedText } from '../i18n/scoped-text';
import { IDENTITY_I18N } from '../i18n/scope';
import { SessionStore } from '../session/session-store.service';
import { safeReturnUrl } from './return-url';

interface LoginHistoryState {
  readonly email?: unknown;
  readonly noticeKey?: unknown;
}

/** Email and password sign in through the Kratos browser flow; outside the shell. */
@Component({
  selector: 'app-login-page',
  imports: [ReactiveFormsModule, ButtonModule, InputTextModule, MessageModule, TranslocoModule],
  providers: [provideTranslocoScope(IDENTITY_I18N)],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './login-page.component.html'
})
export class LoginPage {
  private readonly kratos = inject(KratosLoginClient);
  private readonly store = inject(SessionStore);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);
  private readonly transloco = inject(TranslocoService);
  private readonly historyState = this.readHistoryState(inject(DOCUMENT));

  protected readonly form = new FormGroup({
    email: new FormControl(typeof this.historyState.email === 'string' ? this.historyState.email : '', { nonNullable: true }),
    password: new FormControl('', { nonNullable: true })
  });

  protected readonly isSubmitting = signal(false);
  protected readonly errors = signal<readonly string[]>([]);
  protected readonly notice = signal<string | null>(null);
  protected readonly hasErrors = computed(() => this.errors().length > 0);

  constructor() {
    void this.showNotice();
  }

  protected async submit(): Promise<void> {
    if (this.isSubmitting()) {
      return;
    }

    this.isSubmitting.set(true);
    this.errors.set([]);

    try {
      const texts = await this.signIn();

      this.errors.set(texts);
    } finally {
      this.isSubmitting.set(false);
    }
  }

  /** Runs the flow; resolves with the error texts to show, empty after a successful navigation. */
  private async signIn(): Promise<string[]> {
    const result = await this.kratos.signIn(this.form.controls.email.value, this.form.controls.password.value);

    if (result.kind === 'unavailable') {
      return [await scopedText(this.transloco, IDENTITY_I18N, 'login.unavailable')];
    }

    if (result.kind === 'rejected') {
      return this.rejectedTexts(result.messages);
    }

    try {
      await this.store.reload();
    } catch {
      return [await scopedText(this.transloco, IDENTITY_I18N, 'login.unavailable')];
    }

    if (this.store.state() !== 'authenticated') {
      // Kratos accepted the password but the backend refuses the account; the locked text shows only when the store recorded that denial.
      const denial = this.store.takeDenial();

      const key = denial === null ? 'login.unavailable' : `errors.${denial}`;

      return [await scopedText(this.transloco, IDENTITY_I18N, key)];
    }

    const returnUrl = safeReturnUrl(this.route.snapshot.queryParamMap.get('returnUrl'));

    await this.router.navigateByUrl(returnUrl);

    return [];
  }

  private async rejectedTexts(messages: readonly KratosMessage[]): Promise<string[]> {
    if (messages.length === 0) {
      return [await scopedText(this.transloco, IDENTITY_I18N, 'login.rejected')];
    }

    const texts: string[] = [];

    for (const message of messages) {
      const key = `login.kratos.${message.id}`;

      const mapped = await scopedText(this.transloco, IDENTITY_I18N, key);

      // A key without its own entry comes back unchanged; the Kratos text is shown as text instead.
      texts.push(mapped.endsWith(key) ? message.text : mapped);
    }

    return texts;
  }

  private async showNotice(): Promise<void> {
    const denial = this.store.takeDenial();

    if (denial !== null) {
      this.notice.set(await scopedText(this.transloco, IDENTITY_I18N, `errors.${denial}`));

      return;
    }

    const noticeKey = this.historyState.noticeKey;

    if (typeof noticeKey !== 'string') {
      return;
    }

    const key = `login.notices.${noticeKey}`;

    const text = await scopedText(this.transloco, IDENTITY_I18N, key);

    this.notice.set(text.endsWith(key) ? null : text);
  }

  private readHistoryState(document: Document): LoginHistoryState {
    const state: unknown = document.defaultView?.history.state;

    const isObject = typeof state === 'object' && state !== null;

    return isObject ? (state as LoginHistoryState) : {};
  }
}
