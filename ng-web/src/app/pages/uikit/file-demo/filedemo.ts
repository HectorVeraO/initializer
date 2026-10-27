import { Component, inject, ChangeDetectionStrategy } from '@angular/core';
import { MessageService } from '@openng/optimus-ui/api';
import { ButtonModule } from '@openng/optimus-ui/button';
import { FileUploadModule } from '@openng/optimus-ui/fileupload';
import { ToastModule } from '@openng/optimus-ui/toast';

@Component({
  selector: 'p-file-demo',
  standalone: true,
  imports: [FileUploadModule, ToastModule, ButtonModule],
  templateUrl: './filedemo.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  providers: [MessageService]
})
export class FileDemo {
  uploadedFiles: File[] = [];

  private messageService = inject(MessageService);

  onUpload(event: { files: File[] }) {
    for (const file of event.files) {
      this.uploadedFiles.push(file);
    }

    this.messageService.add({ severity: 'info', summary: 'Success', detail: 'File Uploaded' });
  }

  onBasicUpload() {
    this.messageService.add({ severity: 'info', summary: 'Success', detail: 'File Uploaded with Basic Mode' });
  }
}
