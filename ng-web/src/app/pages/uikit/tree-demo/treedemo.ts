import { Component, inject, OnInit, ChangeDetectionStrategy, signal } from '@angular/core';
import { TreeNode } from '@openng/optimus-ui/api';
import { TreeModule } from '@openng/optimus-ui/tree';
import { FormsModule } from '@angular/forms';
import { TreeTableModule } from '@openng/optimus-ui/treetable';

import { NodeService } from '../../service/node.service';

@Component({
  selector: 'p-tree-demo',
  standalone: true,
  imports: [FormsModule, TreeModule, TreeTableModule],
  templateUrl: './treedemo.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  providers: [NodeService]
})
export class TreeDemo implements OnInit {
  readonly treeValue = signal<TreeNode[]>([]);

  readonly treeTableValue = signal<TreeNode[]>([]);

  selectedTreeValue: TreeNode[] = [];

  selectedTreeTableValue = {};

  cols: { field: string; header: string }[] = [];

  nodeService = inject(NodeService);

  ngOnInit() {
    this.nodeService.getFiles().then((files) => this.treeValue.set(files));
    this.nodeService.getTreeTableNodes().then((files) => this.treeTableValue.set(files));

    this.cols = [
      { field: 'name', header: 'Name' },
      { field: 'size', header: 'Size' },
      { field: 'type', header: 'Type' }
    ];

    this.selectedTreeTableValue = {
      '0-0': {
        partialChecked: false,
        checked: true
      }
    };
  }
}
